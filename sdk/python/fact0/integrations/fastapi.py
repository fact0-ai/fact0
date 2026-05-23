"""FastAPI middleware for request-level audit logging."""

from __future__ import annotations

import time
from typing import Callable

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import Response

from fact0.audit.client import AuditClient


class AuditMiddleware(BaseHTTPMiddleware):
    """Log each HTTP request as an audit event."""

    def __init__(
        self,
        app,
        client_factory: Callable[[], AuditClient],
        *,
        action_prefix: str = "api",
    ):
        super().__init__(app)
        self._client_factory = client_factory
        self._action_prefix = action_prefix

    async def dispatch(self, request: Request, call_next) -> Response:
        start = time.perf_counter()
        response = await call_next(request)
        duration_ms = int((time.perf_counter() - start) * 1000)
        client = self._client_factory()
        client.log(
            actor={"id": "api", "type": "system"},
            action=f"{self._action_prefix}.request",
            resource={"id": request.url.path, "type": "http.route"},
            outcome="success" if response.status_code < 400 else "failure",
            metadata={
                "method": request.method,
                "status": response.status_code,
                "duration_ms": duration_ms,
            },
        )
        return response
