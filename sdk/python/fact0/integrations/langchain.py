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
            self._spans[run_id] = span

    def on_llm_end(self, response: Any, **kwargs: Any) -> None:
        run_id = kwargs.get("run_id")
        span = self._spans.pop(run_id, None) if run_id else None
        if span:
            span.complete(output={"text": str(response)[:500]})

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
            span.complete(output={"result": output[:500]})
