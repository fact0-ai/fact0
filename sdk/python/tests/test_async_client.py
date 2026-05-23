"""Tests for the async audit client."""

from __future__ import annotations

import asyncio

import pytest

from fact0.audit.async_client import AsyncAuditClient
from fact0.exceptions import ValidationError


def _valid(**overrides) -> dict:
    base = {
        "actor": {"id": "u1", "type": "human"},
        "action": "doc.read",
        "resource": {"id": "doc_1", "type": "document"},
        "outcome": "success",
    }
    base.update(overrides)
    return base


def test_async_log_and_flush(mock_server) -> None:
    async def run() -> None:
        client = AsyncAuditClient(mock_server.url, "alk_live_test")
        try:
            await client.log(**_valid())
            await client.close()
        finally:
            await client.close()

    asyncio.run(run())

    assert len(mock_server.received) == 1
    req = mock_server.received[0]
    assert req["method"] == "POST"
    assert req["path"] == "/v1/events/batch"
    assert req["json"]["events"][0]["action"] == "doc.read"


def test_async_validation_error() -> None:
    async def run() -> None:
        client = AsyncAuditClient("http://127.0.0.1:9", "alk_live_test")
        with pytest.raises(ValidationError):
            await client.log(**_valid(outcome="maybe"))
        await client.close()

    asyncio.run(run())
