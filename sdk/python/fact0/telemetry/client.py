"""Execution telemetry REST client."""

from __future__ import annotations

import asyncio
import logging
import queue
import threading
from typing import Any

from .._http import SyncHTTP, AsyncHTTP

_log = logging.getLogger("fact0")


class TelemetryClient:
    def __init__(self, http: SyncHTTP, audit_client: Any | None = None):
        self._http = http
        self._audit = audit_client
        if self._audit is None:
            from ..audit.client import AuditClient
            self._audit = AuditClient(http)
        self._queue: queue.Queue[Any] = queue.Queue()
        self._stopped = False
        self._worker = threading.Thread(target=self._process_queue, daemon=True, name="fact0-telemetry-worker")
        self._worker.start()

    def _process_queue(self) -> None:
        while True:
            try:
                task = self._queue.get()
                if task is None:
                    self._queue.task_done()
                    break
                
                fn, args, kwargs = task
                try:
                    fn(*args, **kwargs)
                except Exception as e:
                    _log.warning("Telemetry background request failed: %s", e)
                finally:
                    self._queue.task_done()
            except Exception:
                pass

    def flush(self) -> None:
        self._queue.join()

    def close(self) -> None:
        if self._stopped:
            return
        self._stopped = True
        self.flush()
        self._queue.put(None)
        self._worker.join(timeout=5.0)

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
        if self._stopped:
            return {}
        self._queue.put((self._ingest_spans_sync, (execution_id, spans), {}))
        return {}

    def _ingest_spans_sync(self, execution_id: str, spans: list[dict[str, Any]]) -> dict[str, Any]:
        return self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/spans",
            json_body={"spans": spans},
        )

    def ingest_events(self, execution_id: str, events: list[dict[str, Any]]) -> dict[str, Any]:
        if self._stopped:
            return {}
        self._queue.put((self._ingest_events_sync, (execution_id, events), {}))
        return {}

    def _ingest_events_sync(self, execution_id: str, events: list[dict[str, Any]]) -> dict[str, Any]:
        return self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/events",
            json_body={"events": events},
        )

    def end_execution(self, execution_id: str, status: str) -> dict[str, Any]:
        if self._stopped:
            return {}
        self._queue.put((self._end_execution_sync, (execution_id, status), {}))
        return {}

    def _end_execution_sync(self, execution_id: str, status: str) -> dict[str, Any]:
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


class AsyncTelemetryClient:
    def __init__(self, http: AsyncHTTP, audit_client: Any | None = None):
        self._http = http
        self._audit = audit_client
        if self._audit is None:
            from ..audit.async_client import AsyncAuditClient
            self._audit = AsyncAuditClient(http.base_url, http.api_key or "")
        self._queue: asyncio.Queue[Any] = asyncio.Queue()
        self._stopped = False
        self._task: asyncio.Task[Any] | None = None

    def start_background_worker(self) -> None:
        if self._task is None or self._task.done():
            self._task = asyncio.create_task(self._process_queue())

    async def _process_queue(self) -> None:
        while True:
            try:
                task = await self._queue.get()
                if task is None:
                    self._queue.task_done()
                    break
                
                fn, args, kwargs = task
                try:
                    await fn(*args, **kwargs)
                except Exception as e:
                    _log.warning("Telemetry background request failed: %s", e)
                finally:
                    self._queue.task_done()
            except asyncio.CancelledError:
                break
            except Exception:
                pass

    async def flush(self) -> None:
        await self._queue.join()

    async def close(self) -> None:
        if self._stopped:
            return
        self._stopped = True
        await self.flush()
        await self._queue.put(None)
        if self._task is not None:
            try:
                await asyncio.wait_for(self._task, timeout=5.0)
            except asyncio.TimeoutError:
                self._task.cancel()
            self._task = None
        await self._http.close()

    async def start_execution(
        self,
        *,
        agent_id: str,
        agent_name: str = "",
        trigger: str = "",
        metadata: dict[str, str] | None = None,
        idempotency_key: str = "",
    ) -> dict[str, Any]:
        self.start_background_worker()
        body: dict[str, Any] = {"agent_id": agent_id}
        if agent_name:
            body["agent_name"] = agent_name
        if trigger:
            body["trigger"] = trigger
        if metadata:
            body["metadata"] = metadata
        if idempotency_key:
            body["idempotency_key"] = idempotency_key
        return await self._http.request("POST", "/api/v1/executions", json_body=body)

    async def ingest_spans(self, execution_id: str, spans: list[dict[str, Any]]) -> dict[str, Any]:
        if self._stopped:
            return {}
        self.start_background_worker()
        await self._queue.put((self._ingest_spans_async, (execution_id, spans), {}))
        return {}

    async def _ingest_spans_async(self, execution_id: str, spans: list[dict[str, Any]]) -> dict[str, Any]:
        return await self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/spans",
            json_body={"spans": spans},
        )

    async def ingest_events(self, execution_id: str, events: list[dict[str, Any]]) -> dict[str, Any]:
        if self._stopped:
            return {}
        self.start_background_worker()
        await self._queue.put((self._ingest_events_async, (execution_id, events), {}))
        return {}

    async def _ingest_events_async(self, execution_id: str, events: list[dict[str, Any]]) -> dict[str, Any]:
        return await self._http.request(
            "POST",
            f"/api/v1/executions/{execution_id}/events",
            json_body={"events": events},
        )

    async def end_execution(self, execution_id: str, status: str) -> dict[str, Any]:
        if self._stopped:
            return {}
        self.start_background_worker()
        await self._queue.put((self._end_execution_async, (execution_id, status), {}))
        return {}

    async def _end_execution_async(self, execution_id: str, status: str) -> dict[str, Any]:
        return await self._http.request(
            "PUT",
            f"/api/v1/executions/{execution_id}/end",
            json_body={"status": status},
        )

    async def list_executions(self, **params: Any) -> dict[str, Any]:
        return await self._http.request("GET", "/api/v1/executions", params=params)

    async def get_execution(self, execution_id: str) -> dict[str, Any]:
        return await self._http.request("GET", f"/api/v1/executions/{execution_id}")

    async def get_spans(self, execution_id: str) -> dict[str, Any]:
        return await self._http.request("GET", f"/api/v1/executions/{execution_id}/spans")

    async def get_dag(self, execution_id: str) -> dict[str, Any]:
        return await self._http.request("GET", f"/api/v1/executions/{execution_id}/dag")

    async def replay(self, execution_id: str, **params: Any) -> dict[str, Any]:
        return await self._http.request(
            "GET", f"/api/v1/executions/{execution_id}/replay", params=params
        )

    async def get_span(self, span_id: str) -> dict[str, Any]:
        return await self._http.request("GET", f"/api/v1/spans/{span_id}")

    def execution(self, **kwargs: Any):
        from .context import AsyncExecutionContext

        return AsyncExecutionContext(self, **kwargs)
