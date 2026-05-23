"""Tests for shared HTTP utilities."""

from __future__ import annotations

import pytest

from fact0._http import SyncHTTP, env_api_key, env_base_url
from fact0.exceptions import TransportError


def test_sync_http_sends_auth_and_user_agent(mock_server) -> None:
    http = SyncHTTP(mock_server.url, "alk_live_test", max_retries=0)
    http.request("GET", "/v1/events")

    req = mock_server.received[0]
    assert req["method"] == "GET"
    assert req["auth"] == "Bearer alk_live_test"


def test_sync_http_sync_ingest_header(mock_server) -> None:
    http = SyncHTTP(mock_server.url, "alk_live_test", sync_ingest=True, max_retries=0)
    http.request("POST", "/v1/events/batch", json_body={"events": []})

    assert mock_server.received[0]["sync"] == "true"


def test_sync_http_raises_on_client_error(mock_server) -> None:
    mock_server.set_responder(lambda _: (400, b'{"error":"bad request"}'))
    http = SyncHTTP(mock_server.url, "alk_live_test", max_retries=0, backoff_base_s=0.01)

    with pytest.raises(TransportError) as exc:
        http.request("GET", "/v1/events")

    assert exc.value.status_code == 400


def test_sync_http_retries_then_succeeds(mock_server) -> None:
    state = {"calls": 0}

    def responder(_: dict) -> tuple[int, bytes]:
        state["calls"] += 1
        if state["calls"] == 1:
            return 503, b'{"error":"unavailable"}'
        return 200, b'{"ok": true}'

    mock_server.set_responder(responder)
    http = SyncHTTP(mock_server.url, "alk_live_test", max_retries=2, backoff_base_s=0.01)

    body = http.request("GET", "/v1/events")
    assert body == {"ok": True}
    assert state["calls"] == 2


def test_env_helpers(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("FACT0_BASE_URL", "https://custom.example/")
    monkeypatch.setenv("FACT0_API_KEY", "alk_live_env")

    assert env_base_url() == "https://custom.example"
    assert env_api_key() == "alk_live_env"
