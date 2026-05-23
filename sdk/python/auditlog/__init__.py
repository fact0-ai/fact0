"""Deprecated auditlog package - re-exports from fact0."""

from __future__ import annotations

import warnings

from .client import (AuditLogError, Client, Transport, TransportError,
                     ValidationError)

warnings.warn(
    "The auditlog package is deprecated; use `pip install fact0-sdk` and `import fact0`.",
    DeprecationWarning,
    stacklevel=2,
)

__all__ = ["Client", "Transport", "TransportError", "ValidationError", "AuditLogError"]
__all__ = ["Client", "Transport", "TransportError", "ValidationError", "AuditLogError"]
