"""Tests for stable event IDs and dead-letter behaviour."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from fact0 import Client
from fact0.exceptions import TransportError
from fact0._http import SyncHTTP


def _valid(**ov):
    base = {
        "actor": {"id": "u1", "type": "human"},
        "action": "doc.read",
        "resource": {"id": "doc_1", "type": "document"},
        "outcome": "success",
    }
    base.update(ov)
    return base


def test_log_generates_stable_event_id(mock_server):
    c = Client(api_key="alk_live_test", base_url=mock_server.url)
    try:
        c.log(**_valid())
        c.flush()
    finally:
        c.close()

    events = mock_server.received[0]["json"]["events"]
    assert events[0].get("id")
    assert len(events[0]["id"]) >= 32


def test_dead_letter_on_failure(mock_server, tmp_path: Path):
    mock_server.set_responder(lambda _: (500, b'{"error":"boom"}'))
    dl = tmp_path / "dead.jsonl"
    c = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        dead_letter_path=str(dl),
        transport=SyncHTTP(
            mock_server.url, "alk_live_test", backoff_base_s=0.01, max_retries=0
        ),
    )
    try:
        c.log(**_valid())
        c.flush()
    finally:
        c.close()

    lines = dl.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    row = json.loads(lines[0])
    assert "error" in row and "event" in row
