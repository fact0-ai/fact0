"""Execution telemetry REST client."""

from __future__ import annotations

from typing import Any

from .._http import SyncHTTP


class TelemetryClient:
    def __init__(self, http: SyncHTTP):
        self._http = http

    def start_execution(
        self,
        *,
        agent_id: str,
        agent_name: str = "",
        trigger: str = "",
        metadata: dict[str, str] | None = None,
        idempotency_key: str = "",
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"agent_id": agent_id}
        if agent_name:
            body["agent_name"] = agent_name
        if trigger:
            body["trigger"] = trigger
        if metadata:
            body["metadata"] = metadata
        if idempotency_key:
            body["idempotency_key"] = idempotency_key
        return self._http.request("POST", "/api/v1/executions", json_body=body)

    def ingest_spans(self, execution_id: str, spans: list[dict[str, Any]]) -> dict[str, Any]:
        return self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/spans",
            json_body={"spans": spans},
        )

    def ingest_events(self, execution_id: str, events: list[dict[str, Any]]) -> dict[str, Any]:
        return self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/events",
            json_body={"events": events},
        )

    def end_execution(self, execution_id: str, status: str) -> dict[str, Any]:
        return self._http.request(
            "PUT",
            f"/api/v1/executions/{execution_id}/end",
            json_body={"status": status},
        )

    def list_executions(self, **params: Any) -> dict[str, Any]:
        return self._http.request("GET", "/api/v1/executions", params=params)

    def get_execution(self, execution_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/api/v1/executions/{execution_id}")

    def get_spans(self, execution_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/api/v1/executions/{execution_id}/spans")

    def get_dag(self, execution_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/api/v1/executions/{execution_id}/dag")

    def replay(self, execution_id: str, **params: Any) -> dict[str, Any]:
        return self._http.request(
            "GET", f"/api/v1/executions/{execution_id}/replay", params=params
        )

    def get_span(self, span_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/api/v1/spans/{span_id}")

    def execution(self, **kwargs: Any):
        from .context import ExecutionContext

        return ExecutionContext(self, **kwargs)
