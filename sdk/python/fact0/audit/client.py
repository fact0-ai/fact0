"""Synchronous audit log client with background batching."""

from __future__ import annotations

import atexit
import json
import logging
import threading
import uuid
from datetime import datetime
from pathlib import Path
from typing import Any, Iterator, Optional

from pydantic import ValidationError as PydanticValidationError

from .._http import DEFAULT_TIMEOUT_S, SyncHTTP
from ..exceptions import Fact0Error, TransportError, ValidationError
from .models import Event
from .transport import AuditTransport

_log = logging.getLogger("fact0")

DEFAULT_BATCH_MAX_SIZE = 100
DEFAULT_BATCH_MAX_WAIT_MS = 500


class AuditClient:
    """Buffered, thread-safe audit log client."""

    def __init__(
        self,
        http: SyncHTTP,
        *,
        batch_max_size: int = DEFAULT_BATCH_MAX_SIZE,
        batch_max_wait_ms: int = DEFAULT_BATCH_MAX_WAIT_MS,
        raise_on_error: bool = False,
        dead_letter_path: Optional[str] = None,
        poll_receipts: bool = True,
        transport: Any | None = None,
    ):
        self._http = http
        if transport is not None and hasattr(transport, "_audit"):
            self._transport = transport._audit
        elif isinstance(transport, SyncHTTP):
            self._http = transport
            self._transport = AuditTransport(transport)
        elif transport is not None:
            self._transport = transport
        else:
            self._transport = AuditTransport(http)
        self._batch_max_size = batch_max_size
        self._batch_max_wait_s = batch_max_wait_ms / 1000.0
        self._raise_on_error = raise_on_error
        self._dead_letter_path = dead_letter_path
        self._poll_receipts = poll_receipts

        self._buf: list[dict[str, Any]] = []
        self._cond = threading.Condition()
        self._stopped = False
        self._flusher = threading.Thread(target=self._run, daemon=True, name="fact0-audit-flush")
        self._flusher.start()
        atexit.register(self.close)

    def log(
        self,
        *,
        actor: dict[str, Any],
        action: str,
        resource: dict[str, Any],
        outcome: str,
        metadata: Optional[dict[str, Any]] = None,
        timestamp: Optional[datetime] = None,
        event_id: Optional[str] = None,
    ) -> None:
        wire = self._validate_event(
            actor=actor,
            action=action,
            resource=resource,
            outcome=outcome,
            metadata=metadata,
            timestamp=timestamp,
            event_id=event_id,
        )
        with self._cond:
            was_empty = not self._buf
            self._buf.append(wire)
            if was_empty or len(self._buf) >= self._batch_max_size:
                self._cond.notify_all()

    def log_batch(self, events: list[dict[str, Any]]) -> dict[str, Any]:
        validated = [self._validate_wire(e) for e in events]
        return self._send(validated)

    def flush(self) -> None:
        while True:
            with self._cond:
                if not self._buf:
                    return
                chunk = self._buf[: self._batch_max_size]
                del self._buf[: self._batch_max_size]
            self._send(chunk)

    def close(self) -> None:
        with self._cond:
            if self._stopped:
                return
            self._stopped = True
            self._cond.notify_all()
        self._flusher.join(timeout=5.0)
        self.flush()

    def get_event(self, event_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/v1/events/{event_id}")

    def list_events(self, **filters: Any) -> dict[str, Any]:
        params = {k: v for k, v in filters.items() if v is not None}
        return self._http.request("GET", "/v1/events", params=params)

    def get_receipt(self, receipt_id: str) -> dict[str, Any]:
        return self._transport.get_receipt(receipt_id)

    def wait_for_receipt(self, receipt_id: str, *, timeout_s: float = 30.0) -> dict[str, Any]:
        return self._transport.poll_receipt(receipt_id, timeout_s=timeout_s)

    def verify(self, **params: Any) -> dict[str, Any]:
        return self._http.request("GET", "/v1/verify", params={k: v for k, v in params.items() if v is not None})

    def verify_event(self, event_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/v1/events/{event_id}/verify")

    def export_pdf(self, **params: Any) -> bytes:
        return self._http.request(
            "GET", "/v1/export/pdf", params={k: v for k, v in params.items() if v is not None}, expect_json=False
        )

    def export_evidence_pack(self, **params: Any) -> bytes:
        return self._http.request(
            "GET",
            "/v1/export/evidence-pack",
            params={k: v for k, v in params.items() if v is not None},
            expect_json=False,
        )

    def stream_events(self) -> Iterator[dict[str, Any]]:
        import requests

        url = f"{self._http.base_url}/v1/events/stream"
        headers = self._http._headers()
        headers["Accept"] = "text/event-stream"
        with requests.get(url, headers=headers, stream=True, timeout=self._http.timeout_s) as resp:
            resp.raise_for_status()
            data_lines: list[str] = []
            for raw in resp.iter_lines(decode_unicode=True):
                if raw is None:
                    continue
                if raw.startswith("data:"):
                    data_lines.append(raw[5:].strip())
                elif raw == "" and data_lines:
                    payload = "".join(data_lines)
                    data_lines.clear()
                    if payload and payload != "{}":
                        yield json.loads(payload)

    def _validate_event(self, **kwargs: Any) -> dict[str, Any]:
        kwargs.pop("event_id", None)
        if kwargs.get("metadata") is None:
            kwargs.pop("metadata", None)
        if "event_id" in kwargs and kwargs["event_id"]:
            kwargs["id"] = kwargs.pop("event_id")
        elif "id" not in kwargs:
            kwargs["id"] = str(uuid.uuid4())
        return self._validate_wire(kwargs)

    def _validate_wire(self, fields: dict[str, Any]) -> dict[str, Any]:
        try:
            evt = Event(**fields)
        except PydanticValidationError as exc:
            raise ValidationError(str(exc)) from exc
        return evt.model_dump(exclude_none=True, mode="json")

    def _run(self) -> None:
        while True:
            with self._cond:
                if self._stopped:
                    return
                if not self._buf:
                    self._cond.wait()
                    if self._stopped:
                        return
                if len(self._buf) < self._batch_max_size:
                    self._cond.wait(timeout=self._batch_max_wait_s)
                if self._stopped:
                    return
                chunk = self._buf[: self._batch_max_size]
                del self._buf[: self._batch_max_size]
            if chunk:
                self._send(chunk)

    def _send(self, buf: list[dict[str, Any]]) -> dict[str, Any]:
        try:
            result = self._transport.post_batch(buf)
            receipt_id = result.get("receipt_id")
            if receipt_id and self._poll_receipts and result.get("status") == "queued":
                self._transport.poll_receipt(receipt_id)
            return result
        except (TransportError, Fact0Error) as exc:
            self._dead_letter(buf, exc)
            if self._raise_on_error:
                raise
            _log.warning("audit flush dropped %d events: %s", len(buf), exc)
            return {}

    def _dead_letter(self, buf: list[dict[str, Any]], exc: Exception) -> None:
        if not self._dead_letter_path:
            return
        path = Path(self._dead_letter_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as f:
            for evt in buf:
                f.write(json.dumps({"error": str(exc), "event": evt}) + "\n")
