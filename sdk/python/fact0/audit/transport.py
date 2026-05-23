"""Audit HTTP transport."""

from __future__ import annotations

import time
from typing import Any

from .._http import SyncHTTP
from ..exceptions import TransportError


class AuditTransport:
    def __init__(self, http: SyncHTTP):
        self._http = http

    def post_batch(self, events: list[dict[str, Any]]) -> dict[str, Any]:
        return self._http.request("POST", "/v1/events/batch", json_body={"events": events})

    def get_receipt(self, receipt_id: str) -> dict[str, Any]:
        return self._http.request("GET", f"/v1/receipts/{receipt_id}")

    def poll_receipt(
        self,
        receipt_id: str,
        *,
        timeout_s: float = 30.0,
        interval_s: float = 0.2,
    ) -> dict[str, Any]:
        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            body = self.get_receipt(receipt_id)
            if body.get("status") in ("committed", "failed"):
                return body
            time.sleep(interval_s)
        raise TransportError(f"receipt {receipt_id} not settled within {timeout_s}s")
