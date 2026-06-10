"""Tests for the telemetry REST client."""

from __future__ import annotations

from fact0._http import SyncHTTP
from fact0.telemetry.client import TelemetryClient


def test_start_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    result = client.start_execution(
        agent_id="agent_1",
        agent_name="demo-agent",
        trigger="manual",
        metadata={"env": "test"},
    )

    assert result == {"accepted": 0, "rejected": 0, "ids": []}
    req = mock_server.received[0]
    assert req["method"] == "POST"
    assert req["path"] == "/api/v1/executions"
    assert req["json"]["agent_id"] == "agent_1"
    assert req["json"]["agent_name"] == "demo-agent"


def test_end_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    client.end_execution("exec_123", "success")
    client.flush()

    req = mock_server.received[0]
    assert req["method"] == "PUT"
    assert req["path"] == "/api/v1/executions/exec_123/end"
    assert req["json"]["status"] == "success"


def test_get_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    client.get_execution("exec_456")

    req = mock_server.received[0]
    assert req["method"] == "GET"
    assert req["path"] == "/api/v1/executions/exec_456"
