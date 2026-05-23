"""Batching behaviour: 150 events → 100 + 50 in two HTTP calls."""

from __future__ import annotations

import time

import pytest

from fact0 import Client


def _valid(**ov):
    base = {
        "actor": {"id": "u1", "type": "human"},
        "action": "doc.read",
        "resource": {"id": "doc_1", "type": "document"},
        "outcome": "success",
    }
    base.update(ov)
    return base


def test_150_events_split_100_then_50(mock_server):
    c = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        batch_max_size=100,
        batch_max_wait_ms=200,
    )
    try:
        for _ in range(150):
            c.log(**_valid())
        c.close()  # blocks until drained
    finally:
        # close() is idempotent
        c.close()

    received = mock_server.received
    sizes = [len(r["json"]["events"]) for r in received]
    assert len(received) == 2, f"expected 2 batches, got sizes={sizes}"
    assert sizes == [100, 50], f"expected [100, 50], got {sizes}"


def test_time_based_flush(mock_server):
    c = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        batch_max_size=1000,  # never hit by size
        batch_max_wait_ms=100,
    )
    try:
        c.log(**_valid())
        # Wait for the time-based flush plus a small grace period.
        time.sleep(0.4)
        received = mock_server.received
        assert len(received) == 1
        assert len(received[0]["json"]["events"]) == 1
    finally:
        c.close()


def test_retry_on_429_then_success(mock_server):
    state = {"calls": 0}

    def responder(_):
        state["calls"] += 1
        if state["calls"] == 1:
            return 429, b'{"error":"slow down"}'
        return 200, b'{"accepted": 1}'

    mock_server.set_responder(responder)

    from fact0._http import SyncHTTP

    c = Client(
        api_key="alk_live_test",
        base_url=mock_server.url,
        transport=SyncHTTP(
            mock_server.url, "alk_live_test", backoff_base_s=0.01, max_retries=3
        ),
    )
    try:
        c.log(**_valid())
        c.flush()
    finally:
        c.close()

    assert state["calls"] >= 2, "expected at least one retry"
