"""LangChain / LangGraph callback handler for Fact0 telemetry.

One handler covers plain LangChain chains, bare ``llm.invoke()`` calls, and
LangGraph graphs (LangGraph runs on LangChain's callback system):

    from fact0 import Client
    from fact0.integrations.langchain import Fact0CallbackHandler

    client = Client(api_key="f0_live_...")
    handler = Fact0CallbackHandler(
        client,
        agent_id="support-agent",
        session_id="sess-123",   # groups multi-turn conversations
        user_id="user-456",
    )

    graph.stream(inputs, config={"callbacks": [handler]})

Each root run (one ``invoke()``/``stream()`` call) becomes one Fact0
execution; chain/graph-node runs, LLM calls, tool calls, and retriever calls
become nested spans linked via ``parent_span_id``. LLM prompts and
completions are captured as chat-message payloads so the dashboard can render
the conversation.

Per-invoke overrides go through LangChain run metadata::

    chain.invoke(inputs, config={
        "callbacks": [handler],
        "metadata": {"fact0_session_id": "sess-123", "fact0_user_id": "user-456"},
    })
"""

from __future__ import annotations

import json
import threading
import time
from typing import Any, Dict, List, Optional

from fact0 import Client

try:  # pragma: no cover - exercised implicitly by real-LangChain tests
    from langchain_core.callbacks.base import BaseCallbackHandler as _LCBaseCallbackHandler

    _HAS_LANGCHAIN_CORE = True
except ImportError:  # langchain-core not installed
    _HAS_LANGCHAIN_CORE = False

    class _LCBaseCallbackHandler:  # type: ignore[no-redef]
        """Shim so this module imports without langchain-core installed.

        Mirrors the attributes LangChain's CallbackManager reads off a
        handler. Installing ``langchain-core`` replaces this with the real
        base class.
        """

        raise_error = False
        run_inline = False
        ignore_llm = False
        ignore_chain = False
        ignore_agent = False
        ignore_retriever = False
        ignore_chat_model = False
        ignore_custom_event = False


# Payloads larger than this are truncated before inline ingestion. The
# original size is preserved in PayloadRef.size_bytes.
MAX_INLINE_BYTES = 64_000

# LangChain message types -> chat roles the dashboard renders.
_ROLE_MAP = {
    "human": "user",
    "ai": "assistant",
    "system": "system",
    "tool": "tool",
    "function": "function",
    "chat": "user",
}

# Run-metadata keys forwarded verbatim onto MODEL_INVOCATION details.
_MODEL_INVOCATION_METADATA_KEYS = (
    "session_id",
    "turn_sequence",
    "prompt_name",
    "prompt_version",
    "cost_usd",
)

# Runs tagged by LangChain as internal plumbing get no span of their own.
_HIDDEN_TAG = "langsmith:hidden"


def _utf8_size(text: str) -> int:
    return len(text.encode("utf-8", errors="replace"))


def _json_default(obj: Any) -> str:
    return str(obj)


def _payload(data: Any, content_type: str = "application/json") -> dict[str, Any]:
    """Build a PayloadRef dict, truncating oversized content."""
    if isinstance(data, str):
        raw, size = data, _utf8_size(data)
        if size > MAX_INLINE_BYTES:
            raw = data[:MAX_INLINE_BYTES] + "…[truncated]"
        return {"inline": raw, "size_bytes": size, "content_type": content_type or "text/plain"}

    serialized = json.dumps(data, default=_json_default)
    size = _utf8_size(serialized)
    inline: Any = data
    if size > MAX_INLINE_BYTES:
        inline = serialized[:MAX_INLINE_BYTES] + "…[truncated]"
        content_type = "text/plain"
    return {"inline": inline, "size_bytes": size, "content_type": content_type}


def _content_to_plain(content: Any) -> Any:
    """Normalize message content (str or multimodal part list) for JSON."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for part in content:
            if isinstance(part, dict):
                parts.append(part)
            else:
                parts.append(str(part))
        return parts
    return str(content)


def _message_to_dict(message: Any) -> dict[str, Any]:
    """Convert a LangChain BaseMessage (or plain dict) into {role, content}."""
    if isinstance(message, dict):
        role = message.get("role") or message.get("type") or "user"
        d: dict[str, Any] = {
            "role": _ROLE_MAP.get(role, role),
            "content": _content_to_plain(message.get("content", "")),
        }
        if message.get("tool_calls"):
            d["tool_calls"] = message["tool_calls"]
        return d

    role = getattr(message, "type", "user")
    d = {
        "role": _ROLE_MAP.get(role, role),
        "content": _content_to_plain(getattr(message, "content", "")),
    }
    tool_calls = getattr(message, "tool_calls", None)
    if tool_calls:
        d["tool_calls"] = [
            tc if isinstance(tc, dict) else {"name": getattr(tc, "name", ""), "args": getattr(tc, "args", {})}
            for tc in tool_calls
        ]
    name = getattr(message, "name", None)
    if name:
        d["name"] = name
    return d


def _truncate(text: str, limit: int = 2000) -> str:
    return text if len(text) <= limit else text[:limit] + "…[truncated]"


class _RunState:
    """Bookkeeping for one LangChain run (chain node, LLM call, tool call)."""

    __slots__ = ("span", "root_key", "parent_span_id", "started_at", "info")

    def __init__(
        self,
        span: Any,
        root_key: Any,
        parent_span_id: Optional[str],
        info: Optional[dict[str, Any]] = None,
    ):
        self.span = span  # SpanContext, or None for hidden runs
        self.root_key = root_key
        self.parent_span_id = parent_span_id
        self.started_at = time.monotonic()
        self.info = info or {}


class Fact0CallbackHandler(_LCBaseCallbackHandler):
    """Record LangChain / LangGraph runs as Fact0 executions and spans.

    Args:
        client: A ``fact0.Client`` (sync).
        agent_id: Stable identifier for the agent being traced.
        agent_name: Human-readable agent name (defaults to the root run name).
        session_id: Conversation/session ID — groups multi-turn invocations.
        user_id: End-user identifier attached to the execution.
        tags: Free-form tags attached to execution metadata.
        trigger: Execution trigger label (default ``"langchain"``).
        audit_sensitive_actions: Also write audit-chain events for run
            start/end and each completed span.
        execution_metadata: Extra key-value pairs merged into every
            execution's metadata.
    """

    # LangChain reads these off the handler; explicit here so the shim path
    # (no langchain-core) behaves identically.
    raise_error = False
    run_inline = False

    def __init__(
        self,
        client: Client,
        *,
        agent_id: str,
        agent_name: str = "",
        session_id: str = "",
        user_id: str = "",
        tags: Optional[List[str]] = None,
        trigger: str = "langchain",
        audit_sensitive_actions: bool = False,
        execution_metadata: Optional[Dict[str, str]] = None,
    ):
        super().__init__()
        self._client = client
        self._agent_id = agent_id
        self._agent_name = agent_name
        self._session_id = session_id
        self._user_id = user_id
        self._tags = list(tags or [])
        self._trigger = trigger
        self._audit_sensitive = audit_sensitive_actions
        self._metadata = dict(execution_metadata or {})

        self._lock = threading.Lock()
        self._runs: dict[Any, _RunState] = {}
        self._executions: dict[Any, Any] = {}  # root run_id -> ExecutionContext

    # ── run bookkeeping ──────────────────────────────────────────────────

    def _start_run(
        self,
        *,
        run_id: Any,
        parent_run_id: Any,
        name: str,
        span_type: str,
        run_tags: Optional[List[str]],
        run_metadata: Optional[Dict[str, Any]],
        info: Optional[dict[str, Any]] = None,
    ) -> Optional[_RunState]:
        """Create the execution (for root runs) and a span for this run."""
        try:
            with self._lock:
                parent_state = self._runs.get(parent_run_id) if parent_run_id else None

                if parent_state is None:
                    # Root run: one execution per invoke()/stream() call.
                    execution = self._client.telemetry.execution(
                        agent_id=self._agent_id,
                        agent_name=self._agent_name or name,
                        trigger=self._trigger,
                        metadata=self._execution_metadata(run_metadata),
                    ).__enter__()
                    self._executions[run_id] = execution
                    root_key = run_id
                    parent_span_id = None
                else:
                    root_key = parent_state.root_key
                    execution = self._executions.get(root_key)
                    if execution is None:
                        return None
                    parent_span_id = (
                        parent_state.span.id if parent_state.span is not None else parent_state.parent_span_id
                    )

                hidden = bool(run_tags) and _HIDDEN_TAG in (run_tags or [])
                span = None
                if not hidden:
                    span = execution.span(name, span_type=span_type, parent_span_id=parent_span_id)
                    span.__enter__()

                state = _RunState(
                    span,
                    root_key,
                    parent_span_id if span is None else span.id,
                    info=info,
                )
                if span is None:
                    # Hidden run: children attach to this run's parent span.
                    state.parent_span_id = parent_span_id
                self._runs[run_id] = state

            if parent_state is None and self._audit_sensitive:
                self._audit_log(
                    action="agent.run.start",
                    resource={"id": execution.id, "type": "agent.execution"},
                )
            return state
        except Exception:
            # Never break the user's chain because telemetry failed.
            return None

    def _pop_run(self, run_id: Any) -> Optional[_RunState]:
        with self._lock:
            return self._runs.pop(run_id, None)

    def _finish_root_if_needed(self, run_id: Any, state: _RunState, status: str) -> None:
        """End the execution when the root run finishes."""
        if state.root_key != run_id:
            return
        with self._lock:
            execution = self._executions.pop(run_id, None)
        if execution is None:
            return
        try:
            self._client.telemetry.end_execution(execution.id, status)
        except Exception:
            pass
        if self._audit_sensitive:
            self._audit_log(
                action="agent.run.end",
                resource={"id": execution.id, "type": "agent.execution"},
                outcome="success" if status == "COMPLETED" else "failure",
            )

    def _execution_metadata(self, run_metadata: Optional[Dict[str, Any]]) -> dict[str, str]:
        md = dict(self._metadata)
        run_metadata = run_metadata or {}
        session_id = (
            run_metadata.get("fact0_session_id")
            or run_metadata.get("session_id")
            or self._session_id
        )
        user_id = (
            run_metadata.get("fact0_user_id")
            or run_metadata.get("user_id")
            or self._user_id
        )
        if session_id:
            md["session_id"] = str(session_id)
        if user_id:
            md["user_id"] = str(user_id)
        if self._tags:
            md.setdefault("tags", ",".join(self._tags))
        return md

    def _session_for_run(self, run_metadata: Optional[Dict[str, Any]]) -> str:
        run_metadata = run_metadata or {}
        return str(
            run_metadata.get("fact0_session_id")
            or run_metadata.get("session_id")
            or self._session_id
            or ""
        )

    def _audit_log(self, *, action: str, resource: dict[str, Any], outcome: str = "success", metadata: Optional[dict[str, Any]] = None) -> None:
        try:
            self._client.audit.log(
                actor={"id": self._agent_id, "type": "agent"},
                action=action,
                resource=resource,
                outcome=outcome,
                metadata=metadata,
            )
        except Exception:
            pass

    @staticmethod
    def _elapsed_ms(state: _RunState) -> int:
        return int((time.monotonic() - state.started_at) * 1000)

    # ── chains & LangGraph nodes ─────────────────────────────────────────

    def on_chain_start(
        self,
        serialized: Optional[Dict[str, Any]],
        inputs: Any,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        if run_id is None:
            return
        metadata = metadata or {}
        name = (
            kwargs.get("name")
            or metadata.get("langgraph_node")
            or (serialized or {}).get("name")
            or ((serialized or {}).get("id") or ["chain"])[-1]
        )
        state = self._start_run(
            run_id=run_id,
            parent_run_id=parent_run_id,
            name=str(name),
            span_type="CUSTOM",
            run_tags=tags,
            run_metadata=metadata,
        )
        if state is not None:
            try:
                state.info["input"] = _truncate(json.dumps(inputs, default=_json_default))
            except Exception:
                state.info["input"] = _truncate(str(inputs))

    def on_chain_end(
        self,
        outputs: Any,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            try:
                output_str = _truncate(json.dumps(outputs, default=_json_default))
            except Exception:
                output_str = _truncate(str(outputs))
            md = {"output": output_str}
            if "input" in state.info:
                md["input"] = state.info["input"]
            state.span.complete(status="COMPLETED", metadata=md, audit=False)
        self._finish_root_if_needed(run_id, state, "COMPLETED")

    def on_chain_error(
        self,
        error: BaseException,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            state.span.complete(
                status="FAILED",
                error={"code": type(error).__name__, "message": _truncate(str(error), 1000)},
                audit=self._audit_sensitive,
            )
        self._finish_root_if_needed(run_id, state, "FAILED")

    # ── LLM calls ────────────────────────────────────────────────────────

    def on_chat_model_start(
        self,
        serialized: Optional[Dict[str, Any]],
        messages: List[List[Any]],
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        prompt_payload = None
        try:
            flat = [_message_to_dict(m) for batch in messages for m in batch]
            prompt_payload = _payload({"messages": flat})
        except Exception:
            pass
        self._start_llm_run(
            serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            tags=tags,
            metadata=metadata,
            prompt_payload=prompt_payload,
            invocation_params=kwargs.get("invocation_params") or {},
            name=kwargs.get("name"),
        )

    def on_llm_start(
        self,
        serialized: Optional[Dict[str, Any]],
        prompts: List[str],
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        prompt_payload = _payload("\n\n".join(prompts), "text/plain") if prompts else None
        self._start_llm_run(
            serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            tags=tags,
            metadata=metadata,
            prompt_payload=prompt_payload,
            invocation_params=kwargs.get("invocation_params") or {},
            name=kwargs.get("name"),
        )

    def _start_llm_run(
        self,
        serialized: Optional[Dict[str, Any]],
        *,
        run_id: Any,
        parent_run_id: Any,
        tags: Optional[List[str]],
        metadata: Optional[Dict[str, Any]],
        prompt_payload: Optional[dict[str, Any]],
        invocation_params: Dict[str, Any],
        name: Optional[str],
    ) -> None:
        if run_id is None:
            return
        serialized = serialized or {}
        model = (
            invocation_params.get("model")
            or invocation_params.get("model_name")
            or invocation_params.get("model_id")
            or serialized.get("kwargs", {}).get("model_name")
            or serialized.get("name")
            or "llm"
        )
        provider = self._provider(serialized, invocation_params)
        span_name = str(name or model)
        self._start_run(
            run_id=run_id,
            parent_run_id=parent_run_id,
            name=span_name,
            span_type="MODEL_INVOCATION",
            run_tags=tags,
            run_metadata=metadata,
            info={
                "prompt_payload": prompt_payload,
                "model": str(model),
                "provider": provider,
                "temperature": invocation_params.get("temperature"),
                "run_metadata": dict(metadata or {}),
            },
        )

    @staticmethod
    def _provider(serialized: Dict[str, Any], invocation_params: Dict[str, Any]) -> str:
        lc_type = str(invocation_params.get("_type", ""))
        for known in ("openai", "anthropic", "google", "bedrock", "mistral", "cohere", "ollama", "groq", "azure"):
            if known in lc_type.lower():
                return known
        id_path = serialized.get("id")
        if isinstance(id_path, list):
            joined = ".".join(str(p).lower() for p in id_path)
            for known in ("openai", "anthropic", "google", "bedrock", "mistral", "cohere", "ollama", "groq", "azure"):
                if known in joined:
                    return known
        return "langchain"

    def on_llm_end(
        self,
        response: Any,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is None:
            self._finish_root_if_needed(run_id, state, "COMPLETED")
            return

        info = state.info
        model_name = info.get("model", "unknown")
        prompt_tokens = completion_tokens = total_tokens = 0

        # Completion + usage from the first generation.
        completion_payload = None
        generations = getattr(response, "generations", None) or []
        first = generations[0][0] if generations and generations[0] else None
        if first is not None:
            message = getattr(first, "message", None)
            if message is not None:
                completion_payload = _payload({"messages": [_message_to_dict(message)]})
                usage = getattr(message, "usage_metadata", None)
                if isinstance(usage, dict):
                    prompt_tokens = int(usage.get("input_tokens", 0) or 0)
                    completion_tokens = int(usage.get("output_tokens", 0) or 0)
                    total_tokens = int(usage.get("total_tokens", prompt_tokens + completion_tokens) or 0)
            else:
                text = getattr(first, "text", "") or ""
                if text:
                    completion_payload = _payload(text, "text/plain")

        # Provider-level metadata (OpenAI-style llm_output) wins when present.
        llm_output = getattr(response, "llm_output", None) or {}
        if isinstance(llm_output, dict):
            model_name = llm_output.get("model_name", model_name)
            token_usage = llm_output.get("token_usage") or llm_output.get("usage")
            if isinstance(token_usage, dict):
                prompt_tokens = int(token_usage.get("prompt_tokens", token_usage.get("input_tokens", prompt_tokens)) or 0)
                completion_tokens = int(
                    token_usage.get("completion_tokens", token_usage.get("output_tokens", completion_tokens)) or 0
                )
                total_tokens = int(token_usage.get("total_tokens", prompt_tokens + completion_tokens) or 0)

        model_invocation: dict[str, Any] = {
            "model_name": str(model_name),
            "model_provider": info.get("provider", "langchain"),
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": total_tokens or (prompt_tokens + completion_tokens),
            "latency_ms": self._elapsed_ms(state),
        }
        if info.get("temperature") is not None:
            model_invocation["temperature"] = info["temperature"]

        # Forward session/turn/prompt-catalog/cost metadata from run config.
        run_metadata = info.get("run_metadata") or {}
        for key in _MODEL_INVOCATION_METADATA_KEYS:
            if key in run_metadata:
                model_invocation[key] = run_metadata[key]
        if "session_id" not in model_invocation:
            session_id = self._session_for_run(run_metadata)
            if session_id:
                model_invocation["session_id"] = session_id

        if info.get("prompt_payload"):
            model_invocation["prompt"] = info["prompt_payload"]
        if completion_payload:
            model_invocation["completion"] = completion_payload

        state.span.complete(
            status="COMPLETED",
            model_invocation=model_invocation,
            audit=self._audit_sensitive,
        )
        self._finish_root_if_needed(run_id, state, "COMPLETED")

    def on_llm_error(
        self,
        error: BaseException,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            state.span.complete(
                status="FAILED",
                error={"code": type(error).__name__, "message": _truncate(str(error), 1000)},
                audit=self._audit_sensitive,
            )
        self._finish_root_if_needed(run_id, state, "FAILED")

    # ── tools ────────────────────────────────────────────────────────────

    def on_tool_start(
        self,
        serialized: Optional[Dict[str, Any]],
        input_str: str,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        inputs: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        if run_id is None:
            return
        name = str((serialized or {}).get("name") or kwargs.get("name") or "tool")
        tool_input: Any = inputs if inputs is not None else input_str
        state = self._start_run(
            run_id=run_id,
            parent_run_id=parent_run_id,
            name=name,
            span_type="TOOL_CALL",
            run_tags=tags,
            run_metadata=metadata,
            info={"tool_name": name, "input_payload": _payload(tool_input)},
        )
        if state is not None and self._audit_sensitive:
            self._audit_log(
                action="agent.tool.call",
                resource={"id": name, "type": "tool"},
                metadata={"input": _truncate(str(tool_input), 500)},
            )

    def on_tool_end(
        self,
        output: Any,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            tool_output = output
            # ToolMessage → its content is the interesting part.
            content = getattr(output, "content", None)
            if content is not None:
                tool_output = content
            state.span.complete(
                status="COMPLETED",
                tool_call={
                    "tool_name": state.info.get("tool_name", "tool"),
                    "input": state.info.get("input_payload"),
                    "output": _payload(tool_output if isinstance(tool_output, (str, dict, list)) else str(tool_output)),
                    "duration_ms": self._elapsed_ms(state),
                },
                audit=self._audit_sensitive,
            )
        self._finish_root_if_needed(run_id, state, "COMPLETED")

    def on_tool_error(
        self,
        error: BaseException,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            state.span.complete(
                status="FAILED",
                tool_call={
                    "tool_name": state.info.get("tool_name", "tool"),
                    "input": state.info.get("input_payload"),
                    "duration_ms": self._elapsed_ms(state),
                },
                error={"code": type(error).__name__, "message": _truncate(str(error), 1000)},
                audit=self._audit_sensitive,
            )
        self._finish_root_if_needed(run_id, state, "FAILED")

    # ── retrievers ───────────────────────────────────────────────────────

    def on_retriever_start(
        self,
        serialized: Optional[Dict[str, Any]],
        query: str,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        if run_id is None:
            return
        name = str((serialized or {}).get("name") or kwargs.get("name") or "retriever")
        self._start_run(
            run_id=run_id,
            parent_run_id=parent_run_id,
            name=name,
            span_type="TOOL_CALL",
            run_tags=tags,
            run_metadata=metadata,
            info={"tool_name": name, "input_payload": _payload({"query": query})},
        )

    def on_retriever_end(
        self,
        documents: Any,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            docs = []
            try:
                for doc in list(documents)[:20]:
                    docs.append(
                        {
                            "page_content": _truncate(str(getattr(doc, "page_content", doc)), 1000),
                            "metadata": getattr(doc, "metadata", {}),
                        }
                    )
            except Exception:
                pass
            state.span.complete(
                status="COMPLETED",
                tool_call={
                    "tool_name": state.info.get("tool_name", "retriever"),
                    "input": state.info.get("input_payload"),
                    "output": _payload({"documents": docs, "count": len(docs)}),
                    "duration_ms": self._elapsed_ms(state),
                },
                audit=False,
            )
        self._finish_root_if_needed(run_id, state, "COMPLETED")

    def on_retriever_error(
        self,
        error: BaseException,
        *,
        run_id: Any = None,
        parent_run_id: Any = None,
        **kwargs: Any,
    ) -> None:
        state = self._pop_run(run_id)
        if state is None:
            return
        if state.span is not None:
            state.span.complete(
                status="FAILED",
                error={"code": type(error).__name__, "message": _truncate(str(error), 1000)},
                audit=False,
            )
        self._finish_root_if_needed(run_id, state, "FAILED")
