"""Shared pytest fixtures for the Fact0 SDK tests."""

from __future__ import annotations

import json
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Callable, Iterator

import pytest


class _CapturingHandler(BaseHTTPRequestHandler):
    """HTTP request handler whose response is supplied per-request."""

    server: "_MockServer"  # type: ignore[assignment]

    def _handle(self, method: str) -> None:
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else b""
        try:
            payload = json.loads(body) if body else None
        except ValueError:
            payload = None

        record = {
            "method": method,
            "path": self.path,
            "auth": self.headers.get("Authorization"),
            "sync": self.headers.get("X-Fact0-Sync"),
            "json": payload,
        }
        with self.server.lock:
            self.server.received.append(record)

        if self.server.delay_s:
            time.sleep(self.server.delay_s)

        status, body_bytes = self.server.responder(record)
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body_bytes)))
        self.end_headers()
        self.wfile.write(body_bytes)

    def do_GET(self) -> None:  # noqa: N802 (stdlib API)
        self._handle("GET")

    def do_POST(self) -> None:  # noqa: N802 (stdlib API)
        self._handle("POST")

    def do_PUT(self) -> None:  # noqa: N802 (stdlib API)
        self._handle("PUT")

    def log_message(self, *args: Any, **kwargs: Any) -> None:
        # Silence the test server.
        pass


class _MockServer(HTTPServer):
    received: list[dict[str, Any]]
    lock: threading.Lock
    responder: Callable[[dict[str, Any]], tuple[int, bytes]]
    delay_s: float


@pytest.fixture
def mock_server() -> Iterator["MockServerHandle"]:
    """A throwaway HTTP server that records inbound requests."""
    received: list[dict[str, Any]] = []
    lock = threading.Lock()

    def default_responder(_: dict[str, Any]) -> tuple[int, bytes]:
        return 200, b'{"accepted": 0, "rejected": 0, "ids": []}'

    srv = _MockServer(("127.0.0.1", 0), _CapturingHandler)
    srv.received = received
    srv.lock = lock
    srv.responder = default_responder
    srv.delay_s = 0.0

    thread = threading.Thread(target=srv.serve_forever, daemon=True)
    thread.start()

    handle = MockServerHandle(srv)
    try:
        yield handle
    finally:
        srv.shutdown()
        thread.join(timeout=2.0)


class MockServerHandle:
    def __init__(self, srv: _MockServer):
        self._srv = srv
        host, port = srv.server_address[:2]
        self.url = f"http://{host}:{port}"

    @property
    def received(self) -> list[dict[str, Any]]:
        with self._srv.lock:
            return list(self._srv.received)

    def set_responder(self, fn: Callable[[dict[str, Any]], tuple[int, bytes]]) -> None:
        self._srv.responder = fn

    def set_delay(self, s: float) -> None:
        self._srv.delay_s = s
