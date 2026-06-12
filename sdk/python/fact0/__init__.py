"""Fact0 Python SDK - audit log and execution telemetry."""

from __future__ import annotations

from typing import Optional

from ._http import DEFAULT_BASE_URL, SyncHTTP, AsyncHTTP, env_api_key
from .audit.async_client import AsyncAuditClient
from .audit.client import AuditClient
from .audit.models import Actor, ActorType, Outcome, Resource
from .audit.transport import AuditTransport
from .exceptions import (
    Fact0Error,
    TransportError,
    ValidationError,
)
from .telemetry.client import TelemetryClient, AsyncTelemetryClient

__version__ = "1.0.3"

__all__ = [
    "Client",
    "AsyncClient",
    "AuditClient",
    "AsyncAuditClient",
    "TelemetryClient",
    "AsyncTelemetryClient",
    "Actor",
    "Resource",
    "Outcome",
    "ActorType",
    "Fact0Error",
    "ValidationError",
    "TransportError",
    "__version__",
]


class Client:
    """Unified Fact0 client with audit and telemetry modules."""

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str | None = None,
        sync: bool = False,
        **audit_kwargs,
    ):
        resolved_key = api_key or env_api_key() or ""
        resolved_base = (base_url or DEFAULT_BASE_URL).rstrip("/")
        custom_transport = audit_kwargs.pop("transport", None)
        if custom_transport is not None and isinstance(custom_transport, SyncHTTP):
            http = custom_transport
        else:
            http = SyncHTTP(resolved_base, resolved_key or None, sync_ingest=sync)
            if custom_transport is not None:
                audit_kwargs["transport"] = custom_transport
        self._http = http
        self.audit = AuditClient(http, **audit_kwargs)
        self.telemetry = TelemetryClient(http, audit_client=self.audit)

    def close(self) -> None:
        self.audit.close()
        self.telemetry.close()

    def log(self, **kwargs) -> None:
        self.audit.log(**kwargs)

    def flush(self) -> None:
        self.audit.flush()
        self.telemetry.flush()


class AsyncClient:
    """Async Fact0 client."""

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str | None = None,
        sync: bool = False,
    ):
        resolved_key = api_key or env_api_key() or ""
        if not resolved_key:
            raise ValueError("api_key is required for AsyncClient audit operations")
        resolved_base = (base_url or DEFAULT_BASE_URL).rstrip("/")
        self.audit = AsyncAuditClient(resolved_base, resolved_key, sync=sync)
        self._base_url = resolved_base
        self.telemetry = AsyncTelemetryClient(
            AsyncHTTP(resolved_base, resolved_key, sync_ingest=sync),
            audit_client=self.audit,
        )

    async def close(self) -> None:
        await self.audit.close()
        await self.telemetry.close()

    async def __aenter__(self) -> AsyncClient:
        return self

    async def __aexit__(self, *args) -> None:
        await self.close()
