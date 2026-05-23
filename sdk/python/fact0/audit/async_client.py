"""Async audit client using httpx."""

from __future__ import annotations

import asyncio
import json
import logging
import uuid
from datetime import datetime
from typing import Any, Optional

import httpx
from pydantic import ValidationError as PydanticValidationError

from .._http import DEFAULT_TIMEOUT_S, USER_AGENT
from ..exceptions import TransportError, ValidationError
from .models import Event

_log = logging.getLogger("fact0")

_RETRYABLE = {429, 500, 502, 503, 504}


class AsyncAuditClient:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        timeout_s: float = DEFAULT_TIMEOUT_S,
        sync: bool = False,
        client: httpx.AsyncClient | None = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.sync = sync
        self._client = client
        self._owned = client is None
        self.timeout_s = timeout_s
        self._buf: list[dict[str, Any]] = []

    async def _get_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self.timeout_s)
        return self._client

    def _headers(self) -> dict[str, str]:
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
            "User-Agent": USER_AGENT,
        }
        if self.sync:
            headers["X-Fact0-Sync"] = "true"
        return headers

    async def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any | None = None,
        params: dict[str, Any] | None = None,
        expect_json: bool = True,
    ) -> Any:
        client = await self._get_client()
        url = f"{self.base_url}{path}"
        resp = await client.request(method, url, json=json_body, params=params, headers=self._headers())
        if resp.status_code >= 300:
            raise TransportError(f"{method} {path} -> {resp.status_code}", status_code=resp.status_code)
        if not expect_json:
            return resp.content
        try:
            return resp.json()
        except ValueError:
            return {}

    async def log(self, **fields: Any) -> None:
        wire = self._validate(**fields)
        self._buf.append(wire)

    async def flush(self) -> None:
        if not self._buf:
            return
        chunk, self._buf = self._buf, []
        await self._request("POST", "/v1/events/batch", json_body={"events": chunk})

    async def close(self) -> None:
        await self.flush()
        if self._owned and self._client is not None:
            await self._client.aclose()
            self._client = None

    async def get_event(self, event_id: str) -> dict[str, Any]:
        return await self._request("GET", f"/v1/events/{event_id}")

    async def list_events(self, **filters: Any) -> dict[str, Any]:
        return await self._request("GET", "/v1/events", params={k: v for k, v in filters.items() if v is not None})

    async def verify(self, **params: Any) -> dict[str, Any]:
        return await self._request("GET", "/v1/verify", params={k: v for k, v in params.items() if v is not None})

    async def verify_event(self, event_id: str) -> dict[str, Any]:
        return await self._request("GET", f"/v1/events/{event_id}/verify")

    async def get_receipt(self, receipt_id: str) -> dict[str, Any]:
        return await self._request("GET", f"/v1/receipts/{receipt_id}")

    async def wait_for_receipt(self, receipt_id: str, *, timeout_s: float = 30.0) -> dict[str, Any]:
        import time

        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            body = await self.get_receipt(receipt_id)
            if body.get("status") in ("committed", "failed"):
                return body
            await asyncio.sleep(0.2)
        raise TransportError(f"receipt {receipt_id} not settled within {timeout_s}s")

    def _validate(self, **kwargs: Any) -> dict[str, Any]:
        if kwargs.get("event_id"):
            kwargs["id"] = kwargs.pop("event_id")
        elif "id" not in kwargs:
            kwargs["id"] = str(uuid.uuid4())
        try:
            evt = Event(**kwargs)
        except PydanticValidationError as exc:
            raise ValidationError(str(exc)) from exc
        return evt.model_dump(exclude_none=True, mode="json")
