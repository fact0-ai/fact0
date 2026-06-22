"""LangChain callback handler for Fact0 telemetry and audit."""

from __future__ import annotations

from typing import Any, Dict, List, Optional
from uuid import UUID

from fact0 import Client


class Fact0CallbackHandler:
    """Record LangChain runs to Fact0 telemetry (and optional audit events)."""

    def __init__(
        self,
        client: Client,
        *,
        agent_id: str,
        audit_sensitive_actions: bool = False,
        execution_metadata: dict[str, str] | None = None,
    ):
        self._client = client
        self._agent_id = agent_id
        self._audit_sensitive = audit_sensitive_actions
        self._metadata = execution_metadata or {}
        self._execution_ctx = None
        self._spans: dict[UUID, Any] = {}

    def _ensure_execution(self):
        if self._execution_ctx is None:
            self._execution_ctx = self._client.telemetry.execution(
                agent_id=self._agent_id,
                metadata=self._metadata,
            ).__enter__()
            if self._audit_sensitive:
                self._client.audit.log(
                    actor={"id": self._agent_id, "type": "agent"},
                    action="agent.run.start",
                    resource={"id": self._execution_ctx.id, "type": "agent.execution"},
                    outcome="success",
                )

    # LangChain BaseCallbackHandler-style hooks (duck typed)
    def on_chain_start(self, serialized: dict, inputs: dict, **kwargs: Any) -> None:
        self._ensure_execution()

    def on_chain_end(self, outputs: dict, **kwargs: Any) -> None:
        if self._execution_ctx and self._audit_sensitive:
            self._client.audit.log(
                actor={"id": self._agent_id, "type": "agent"},
                action="agent.run.end",
                resource={"id": self._execution_ctx.id, "type": "agent.execution"},
                outcome="success",
            )
        if self._execution_ctx:
            self._client.telemetry.end_execution(self._execution_ctx.id, "COMPLETED")
            self._execution_ctx = None

    def on_llm_start(self, serialized: dict, prompts: List[str], **kwargs: Any) -> None:
        self._ensure_execution()
        run_id = kwargs.get("run_id")
        if self._execution_ctx and run_id:
            span = self._execution_ctx.span(
                serialized.get("name", "llm"), span_type="MODEL_INVOCATION"
            )
            span.__enter__()
            if prompts:
                setattr(span, "_prompt_text", "\n".join(prompts))
            # Store run metadata if present
            run_metadata = kwargs.get("metadata") or {}
            setattr(span, "_run_metadata", run_metadata)
            self._spans[run_id] = span

    def on_llm_end(self, response: Any, **kwargs: Any) -> None:
        run_id = kwargs.get("run_id")
        span = self._spans.pop(run_id, None) if run_id else None
        if span:
            model_name = "unknown"
            prompt_tokens = 0
            completion_tokens = 0
            total_tokens = 0

            llm_output = getattr(response, "llm_output", None) or {}
            if isinstance(llm_output, dict):
                model_name = llm_output.get("model_name", model_name)
                token_usage = llm_output.get("token_usage")
                if isinstance(token_usage, dict):
                    prompt_tokens = token_usage.get("prompt_tokens", 0)
                    completion_tokens = token_usage.get("completion_tokens", 0)
                    total_tokens = token_usage.get("total_tokens", 0)

            text_output = ""
            generations = getattr(response, "generations", [])
            if generations and len(generations) > 0 and len(generations[0]) > 0:
                text_output = getattr(generations[0][0], "text", "")

            model_invocation: dict[str, Any] = {
                "model_name": model_name,
                "model_provider": "langchain",
                "prompt_tokens": int(prompt_tokens),
                "completion_tokens": int(completion_tokens),
                "total_tokens": int(total_tokens),
            }

            # Forward session/turn/prompt catalog/cost metadata from LangChain run config
            run_metadata = getattr(span, "_run_metadata", None) or {}
            for key in ["session_id", "turn_sequence", "prompt_name", "prompt_version", "cost_usd"]:
                if key in run_metadata:
                    model_invocation[key] = run_metadata[key]

            prompt_text = getattr(span, "_prompt_text", "")
            if prompt_text:
                model_invocation["prompt"] = {
                    "inline": prompt_text,
                    "size_bytes": len(prompt_text),
                    "content_type": "text/plain",
                }

            if text_output:
                model_invocation["completion"] = {
                    "inline": text_output,
                    "size_bytes": len(text_output),
                    "content_type": "text/plain",
                }

            span.complete(
                status="COMPLETED",
                model_invocation=model_invocation,
                audit=self._audit_sensitive,
            )

    def on_tool_start(self, serialized: dict, input_str: str, **kwargs: Any) -> None:
        self._ensure_execution()
        run_id = kwargs.get("run_id")
        name = serialized.get("name", "tool")
        if self._execution_ctx and run_id:
            span = self._execution_ctx.span(name, span_type="TOOL_CALL")
            span.__enter__()
            self._spans[run_id] = span
        if self._audit_sensitive:
            self._client.audit.log(
                actor={"id": self._agent_id, "type": "agent"},
                action="agent.tool.call",
                resource={"id": name, "type": "tool"},
                outcome="success",
                metadata={"input": input_str[:500]},
            )

    def on_tool_end(self, output: str, **kwargs: Any) -> None:
        run_id = kwargs.get("run_id")
        span = self._spans.pop(run_id, None) if run_id else None
        if span:
            span.complete(output={"result": output[:500]}, audit=False)
