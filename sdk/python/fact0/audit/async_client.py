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

from .._http import DEFAULT_TIMEOUT_S, USER_AGENT, AsyncHTTP
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
        self._http = AsyncHTTP(
            base_url,
            api_key,
            timeout_s=timeout_s,
            sync_ingest=sync,
            client=client,
        )
        self._buf: list[dict[str, Any]] = []

    @property
    def base_url(self) -> str:
        return self._http.base_url

    @property
    def api_key(self) -> str:
        return self._http.api_key or ""

    @property
    def sync(self) -> bool:
        return self._http.sync_ingest

    async def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any | None = None,
        params: dict[str, Any] | None = None,
        expect_json: bool = True,
    ) -> Any:
        return await self._http.request(
            method,
            path,
            json_body=json_body,
            params=params,
            expect_json=expect_json,
        )

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
        await self._http.close()

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
