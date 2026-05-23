"""Deprecated auditlog.Client - thin wrapper around fact0.audit.client."""

from __future__ import annotations

import warnings
from typing import Any

from fact0._http import SyncHTTP, env_base_url
from fact0.audit.client import AuditClient
from fact0.exceptions import AuditLogError, TransportError, ValidationError

warnings.warn(
    "The auditlog package is deprecated; use `pip install fact0` and `import fact0`.",
    DeprecationWarning,
    stacklevel=2,
)


class Client:
    """Back-compat audit-only client (api_key + base_url constructor)."""

    def __init__(
        self,
        api_key: str,
        *,
        base_url: str | None = None,
        sync: bool = False,
        transport: Any | None = None,
        **audit_kwargs: Any,
    ):
        resolved_base = (base_url or env_base_url()).rstrip("/")
        if transport is not None and isinstance(transport, SyncHTTP):
            http = transport
        else:
            http = SyncHTTP(resolved_base, api_key, sync_ingest=sync)
            if transport is not None:
                audit_kwargs.setdefault("transport", transport)
        self._audit = AuditClient(http, **audit_kwargs)

    def log(self, **kwargs: Any) -> None:
        self._audit.log(**kwargs)

    def flush(self) -> None:
        self._audit.flush()

    def close(self) -> None:
        self._audit.close()


# Legacy alias: auditlog.Transport was SyncHTTP with positional url/key args.
Transport = SyncHTTP

__all__ = ["Client", "Transport", "TransportError", "ValidationError", "AuditLogError"]
