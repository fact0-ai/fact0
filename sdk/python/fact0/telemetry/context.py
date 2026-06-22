"""Execution and span context managers for telemetry."""

from __future__ import annotations

import uuid
from datetime import datetime, timezone
from typing import Any

from .client import TelemetryClient, AsyncTelemetryClient


def _utcnow() -> datetime:
    return datetime.now(timezone.utc)


class SpanContext:
    def __init__(
        self,
        tel: TelemetryClient,
        execution_id: str,
        name: str,
        span_type: str,
        audit_client: Any | None = None,
        agent_id: str = "unknown",
        parent_span_id: str | None = None,
    ):
        self._tel = tel
        self._execution_id = execution_id
        self._span_id = f"span_{uuid.uuid4().hex[:16]}"
        self._name = name
        self._span_type = span_type
        self._started = _utcnow()
        self._ended = False
        self._audit = audit_client
        self._agent_id = agent_id
        self._parent_span_id = parent_span_id

    def log_event(self, event_type: str, payload: dict[str, Any]) -> None:
        self._tel.ingest_events(
            self._execution_id,
            [
                {
                    "id": f"evt_{uuid.uuid4().hex[:16]}",
                    "execution_id": self._execution_id,
                    "span_id": self._span_id,
                    "event_type": event_type,
                    "timestamp": _utcnow().isoformat(),
                    "payload": payload,
                }
            ],
        )

    def complete(
        self,
        *,
        output: dict[str, Any] | None = None,
        status: str = "COMPLETED",
        model_invocation: dict[str, Any] | None = None,
        tool_call: dict[str, Any] | None = None,
        state_mutation: dict[str, Any] | None = None,
        human_approval: dict[str, Any] | None = None,
        policy_evaluation: dict[str, Any] | None = None,
        metadata: dict[str, str] | None = None,
        audit: bool = True,
    ) -> None:
        """Mark the span complete and record its outcome and details.

        Args:
            output: Optional key-value map representing the output of the span.
            status: Lifecycle status of the span (e.g., "COMPLETED", "FAILED").
            model_invocation: Optional metadata for LLM calls (SpanType: MODEL_INVOCATION).
                Supported keys:
                    - model_name (str): Name of the model.
                    - model_provider (str): Provider (e.g., 'openai', 'anthropic', 'google').
                    - prompt_tokens (int): Count of input tokens.
                    - completion_tokens (int): Count of output tokens.
                    - total_tokens (int): Total token count.
                    - latency_ms (int): Duration of the LLM call in milliseconds.
                    - temperature (float): LLM sampling temperature.
                    - session_id (str): Conversation session thread ID.
                    - turn_sequence (int): 0-indexed turn sequence number.
                    - prompt_name (str): Registered prompt name in prompt catalog.
                    - prompt_version (int): Version of prompt catalog template.
                    - cost_usd (float): Direct override cost in USD.
            tool_call: Optional metadata for external tool calls (SpanType: TOOL_CALL).
            state_mutation: Optional metadata for state mutation events.
            human_approval: Optional metadata for human approval gates.
            policy_evaluation: Optional metadata for policy check runs.
            metadata: Custom key-value pairs to attach to this span.
            audit: Whether to log a corresponding audit log event.
        """
        if self._ended:
            return
        self._ended = True
        span: dict[str, Any] = {
            "id": self._span_id,
            "execution_id": self._execution_id,
            "span_type": self._span_type,
            "name": self._name,
            "status": status,
            "started_at": self._started.isoformat(),
            "ended_at": _utcnow().isoformat(),
        }
        if self._parent_span_id:
            span["parent_span_id"] = self._parent_span_id
        if output:
            span["metadata"] = {"output": str(output)}
        if metadata:
            if "metadata" not in span:
                span["metadata"] = {}
            span["metadata"].update(metadata)
        if model_invocation:
            span["model_invocation"] = model_invocation
        if tool_call:
            span["tool_call"] = tool_call
        if state_mutation:
            span["state_mutation"] = state_mutation
        if human_approval:
            span["human_approval"] = human_approval
        if policy_evaluation:
            span["policy_evaluation"] = policy_evaluation
        self._tel.ingest_spans(self._execution_id, [span])

        if audit and self._audit:
            action_map = {
                "MODEL_INVOCATION": "agent.model.invoke",
                "TOOL_CALL": "agent.tool.call",
                "STATE_MUTATION": "agent.state.update",
                "HUMAN_APPROVAL": "agent.approval.decide",
                "POLICY_EVALUATION": "agent.policy.evaluate",
            }
            action = action_map.get(self._span_type, "agent.span.completed")
            outcome = "success" if status == "COMPLETED" else "failure" if status == "FAILED" else "error"
            audit_metadata = {
                "execution_id": self._execution_id,
                "span_id": self._span_id,
            }
            if output:
                audit_metadata["output"] = str(output)
            if model_invocation:
                audit_metadata["model"] = model_invocation.get("model_name", "unknown")
                audit_metadata["tokens"] = model_invocation.get("total_tokens", 0)
            if tool_call:
                audit_metadata["tool"] = tool_call.get("tool_name", "unknown")
            if policy_evaluation:
                audit_metadata["policy"] = policy_evaluation.get("policy_name", "unknown")
                audit_metadata["result"] = policy_evaluation.get("result", "unknown")

            try:
                self._audit.log(
                    actor={"id": self._agent_id, "type": "agent"},
                    action=action,
                    resource={"id": self._span_id, "type": "span", "name": self._name},
                    outcome=outcome,
                    metadata=audit_metadata,
                )
            except Exception:
                pass

    def __enter__(self) -> SpanContext:
        span: dict[str, Any] = {
            "id": self._span_id,
            "execution_id": self._execution_id,
            "span_type": self._span_type,
            "name": self._name,
            "status": "RUNNING",
            "started_at": self._started.isoformat(),
        }
        if self._parent_span_id:
            span["parent_span_id"] = self._parent_span_id
        self._tel.ingest_spans(self._execution_id, [span])
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        if not self._ended:
            status = "FAILED" if exc_type else "COMPLETED"
            self.complete(status=status)


class ExecutionContext:
    def __init__(self, tel: TelemetryClient, **kwargs: Any):
        self._tel = tel
        self._kwargs = kwargs
        self._execution: dict[str, Any] | None = None

    @property
    def id(self) -> str:
        if not self._execution:
            raise RuntimeError("execution not started")
        return self._execution["id"]

    def span(self, name: str, *, span_type: str = "CUSTOM") -> SpanContext:
        return SpanContext(
            self._tel,
            self.id,
            name,
            span_type,
            audit_client=self._tel._audit,
            agent_id=self._kwargs.get("agent_id", "unknown"),
        )

    def __enter__(self) -> ExecutionContext:
        self._execution = self._tel.start_execution(**self._kwargs)
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        if self._execution:
            status = "FAILED" if exc_type else "COMPLETED"
            self._tel.end_execution(self._execution["id"], status)


class AsyncSpanContext:
    def __init__(
        self,
        tel: AsyncTelemetryClient,
        execution_id: str,
        name: str,
        span_type: str,
        audit_client: Any | None = None,
        agent_id: str = "unknown",
        parent_span_id: str | None = None,
    ):
        self._tel = tel
        self._execution_id = execution_id
        self._span_id = f"span_{uuid.uuid4().hex[:16]}"
        self._name = name
        self._span_type = span_type
        self._started = _utcnow()
        self._ended = False
        self._audit = audit_client
        self._agent_id = agent_id
        self._parent_span_id = parent_span_id

    async def log_event(self, event_type: str, payload: dict[str, Any]) -> None:
        await self._tel.ingest_events(
            self._execution_id,
            [
                {
                    "id": f"evt_{uuid.uuid4().hex[:16]}",
                    "execution_id": self._execution_id,
                    "span_id": self._span_id,
                    "event_type": event_type,
                    "timestamp": _utcnow().isoformat(),
                    "payload": payload,
                }
            ],
        )

    async def complete(
        self,
        *,
        output: dict[str, Any] | None = None,
        status: str = "COMPLETED",
        model_invocation: dict[str, Any] | None = None,
        tool_call: dict[str, Any] | None = None,
        state_mutation: dict[str, Any] | None = None,
        human_approval: dict[str, Any] | None = None,
        policy_evaluation: dict[str, Any] | None = None,
        metadata: dict[str, str] | None = None,
        audit: bool = True,
    ) -> None:
        """Mark the span complete and record its outcome and details asynchronously.

        Args:
            output: Optional key-value map representing the output of the span.
            status: Lifecycle status of the span (e.g., "COMPLETED", "FAILED").
            model_invocation: Optional metadata for LLM calls (SpanType: MODEL_INVOCATION).
                Supported keys:
                    - model_name (str): Name of the model.
                    - model_provider (str): Provider (e.g., 'openai', 'anthropic', 'google').
                    - prompt_tokens (int): Count of input tokens.
                    - completion_tokens (int): Count of output tokens.
                    - total_tokens (int): Total token count.
                    - latency_ms (int): Duration of the LLM call in milliseconds.
                    - temperature (float): LLM sampling temperature.
                    - session_id (str): Conversation session thread ID.
                    - turn_sequence (int): 0-indexed turn sequence number.
                    - prompt_name (str): Registered prompt name in prompt catalog.
                    - prompt_version (int): Version of prompt catalog template.
                    - cost_usd (float): Direct override cost in USD.
            tool_call: Optional metadata for external tool calls (SpanType: TOOL_CALL).
            state_mutation: Optional metadata for state mutation events.
            human_approval: Optional metadata for human approval gates.
            policy_evaluation: Optional metadata for policy check runs.
            metadata: Custom key-value pairs to attach to this span.
            audit: Whether to log a corresponding audit log event.
        """
        if self._ended:
            return
        self._ended = True
        span: dict[str, Any] = {
            "id": self._span_id,
            "execution_id": self._execution_id,
            "span_type": self._span_type,
            "name": self._name,
            "status": status,
            "started_at": self._started.isoformat(),
            "ended_at": _utcnow().isoformat(),
        }
        if self._parent_span_id:
            span["parent_span_id"] = self._parent_span_id
        if output:
            span["metadata"] = {"output": str(output)}
        if metadata:
            if "metadata" not in span:
                span["metadata"] = {}
            span["metadata"].update(metadata)
        if model_invocation:
            span["model_invocation"] = model_invocation
        if tool_call:
            span["tool_call"] = tool_call
        if state_mutation:
            span["state_mutation"] = state_mutation
        if human_approval:
            span["human_approval"] = human_approval
        if policy_evaluation:
            span["policy_evaluation"] = policy_evaluation
        await self._tel.ingest_spans(self._execution_id, [span])

        if audit and self._audit:
            action_map = {
                "MODEL_INVOCATION": "agent.model.invoke",
                "TOOL_CALL": "agent.tool.call",
                "STATE_MUTATION": "agent.state.update",
                "HUMAN_APPROVAL": "agent.approval.decide",
                "POLICY_EVALUATION": "agent.policy.evaluate",
            }
            action = action_map.get(self._span_type, "agent.span.completed")
            outcome = "success" if status == "COMPLETED" else "failure" if status == "FAILED" else "error"
            audit_metadata = {
                "execution_id": self._execution_id,
                "span_id": self._span_id,
            }
            if output:
                audit_metadata["output"] = str(output)
            if model_invocation:
                audit_metadata["model"] = model_invocation.get("model_name", "unknown")
                audit_metadata["tokens"] = model_invocation.get("total_tokens", 0)
            if tool_call:
                audit_metadata["tool"] = tool_call.get("tool_name", "unknown")
            if policy_evaluation:
                audit_metadata["policy"] = policy_evaluation.get("policy_name", "unknown")
                audit_metadata["result"] = policy_evaluation.get("result", "unknown")

            try:
                await self._audit.log(
                    actor={"id": self._agent_id, "type": "agent"},
                    action=action,
                    resource={"id": self._span_id, "type": "span", "name": self._name},
                    outcome=outcome,
                    metadata=audit_metadata,
                )
            except Exception:
                pass

    async def __aenter__(self) -> AsyncSpanContext:
        span: dict[str, Any] = {
            "id": self._span_id,
            "execution_id": self._execution_id,
            "span_type": self._span_type,
            "name": self._name,
            "status": "RUNNING",
            "started_at": self._started.isoformat(),
        }
        if self._parent_span_id:
            span["parent_span_id"] = self._parent_span_id
        await self._tel.ingest_spans(self._execution_id, [span])
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        if not self._ended:
            status = "FAILED" if exc_type else "COMPLETED"
            await self.complete(status=status)


class AsyncExecutionContext:
    def __init__(self, tel: AsyncTelemetryClient, **kwargs: Any):
        self._tel = tel
        self._kwargs = kwargs
        self._execution: dict[str, Any] | None = None

    @property
    def id(self) -> str:
        if not self._execution:
            raise RuntimeError("execution not started")
        return self._execution["id"]

    def span(self, name: str, *, span_type: str = "CUSTOM") -> AsyncSpanContext:
        return AsyncSpanContext(
            self._tel,
            self.id,
            name,
            span_type,
            audit_client=self._tel._audit,
            agent_id=self._kwargs.get("agent_id", "unknown"),
        )

    async def __aenter__(self) -> AsyncExecutionContext:
        self._execution = await self._tel.start_execution(**self._kwargs)
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        if self._execution:
            status = "FAILED" if exc_type else "COMPLETED"
            await self._tel.end_execution(self._execution["id"], status)
