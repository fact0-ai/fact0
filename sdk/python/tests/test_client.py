"""Tests for the synchronous audit client."""

from __future__ import annotations

import json
import time
from pathlib import Path

import pytest

from fact0 import Client
from fact0.exceptions import TransportError, ValidationError
from fact0._http import SyncHTTP


def _actor() -> dict:
    return {"type": "human", "id": "user_123"}


def _resource() -> dict:
    return {"type": "document", "id": "doc_456"}


def test_log_and_flush_sends_batch(mock_server) -> None:
    client = Client(api_key="alk_live_test", base_url=mock_server.url)
    try:
        client.log(actor=_actor(), action="document.read", resource=_resource(), outcome="success")
        client.flush()
    finally:
        client.close()

    assert len(mock_server.received) == 1
    req = mock_server.received[0]
    assert req["path"] == "/v1/events/batch"
    assert req["auth"] == "Bearer alk_live_test"
    assert req["json"]["events"][0]["action"] == "document.read"


def test_validation_error_on_bad_outcome(mock_server) -> None:
    client = Client(api_key="alk_live_test", base_url=mock_server.url)
    try:
        with pytest.raises(ValidationError):
            client.log(
                actor=_actor(),
                action="document.read",
                resource=_resource(),
                outcome="maybe",
            )
    finally:
        client.close()


def test_fail_soft_does_not_raise(mock_server) -> None:
    mock_server.set_responder(lambda _: (500, b'{"error":"boom"}'))

    transport = SyncHTTP(
        mock_server.url,
        "alk_live_test",
        backoff_base_s=0.01,
        max_retries=1,
    )
    client = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        transport=transport,
        raise_on_error=False,
    )
    try:
        client.log(actor=_actor(), action="document.read", resource=_resource(), outcome="success")
        client.flush()
    finally:
        client.close()


def test_fail_hard_raises_transport_error(mock_server) -> None:
    mock_server.set_responder(lambda _: (500, b'{"error":"boom"}'))

    transport = SyncHTTP(
        mock_server.url,
        "alk_live_test",
        backoff_base_s=0.01,
        max_retries=1,
    )
    client = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        transport=transport,
        raise_on_error=True,
    )
    try:
        client.log(actor=_actor(), action="document.read", resource=_resource(), outcome="success")
        with pytest.raises(TransportError):
            client.flush()
    finally:
        client.close()


def test_dead_letter_on_persistent_failure(mock_server, tmp_path: Path) -> None:
    mock_server.set_responder(lambda _: (500, b'{"error":"boom"}'))
    dl = tmp_path / "dead.jsonl"

    transport = SyncHTTP(
        mock_server.url,
        "alk_live_test",
        backoff_base_s=0.01,
        max_retries=1,
    )
    client = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        transport=transport,
        raise_on_error=False,
        dead_letter_path=str(dl),
    )
    try:
        client.log(actor=_actor(), action="document.read", resource=_resource(), outcome="success")
        client.flush()
    finally:
        client.close()

    assert dl.exists()
    lines = dl.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    record = json.loads(lines[0])
    assert record["event"]["action"] == "document.read"


def test_auto_flush_on_batch_size(mock_server) -> None:
    client = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        batch_max_size=2,
        batch_max_wait_ms=60_000,
    )
    try:
        for _ in range(2):
            client.log(
                actor=_actor(),
                action="document.read",
                resource=_resource(),
                outcome="success",
            )
        deadline = time.time() + 2.0
        while time.time() < deadline and len(mock_server.received) < 1:
            time.sleep(0.05)
    finally:
        client.close()

    assert len(mock_server.received) >= 1
