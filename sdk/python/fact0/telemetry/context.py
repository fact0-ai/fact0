"""Execution and span context managers for telemetry."""

from __future__ import annotations

import uuid
from datetime import datetime, timezone
from typing import Any

from .client import TelemetryClient


def _utcnow() -> datetime:
    return datetime.now(timezone.utc)


class SpanContext:
    def __init__(self, tel: TelemetryClient, execution_id: str, name: str, span_type: str):
        self._tel = tel
        self._execution_id = execution_id
        self._span_id = f"span_{uuid.uuid4().hex[:16]}"
        self._name = name
        self._span_type = span_type
        self._started = _utcnow()
        self._ended = False

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

    def complete(self, *, output: dict[str, Any] | None = None, status: str = "COMPLETED") -> None:
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
        if output:
            span["metadata"] = {"output": str(output)}
        self._tel.ingest_spans(self._execution_id, [span])

    def __enter__(self) -> SpanContext:
        self._tel.ingest_spans(
            self._execution_id,
            [
                {
                    "id": self._span_id,
                    "execution_id": self._execution_id,
                    "span_type": self._span_type,
                    "name": self._name,
                    "status": "RUNNING",
                    "started_at": self._started.isoformat(),
                }
            ],
        )
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
        return SpanContext(self._tel, self.id, name, span_type)

    def __enter__(self) -> ExecutionContext:
        self._execution = self._tel.start_execution(**self._kwargs)
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        if self._execution:
            status = "FAILED" if exc_type else "COMPLETED"
            self._tel.end_execution(self._execution["id"], status)
