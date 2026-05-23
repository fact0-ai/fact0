"""Public exception types raised by the Fact0 SDK."""

from __future__ import annotations


class Fact0Error(Exception):
    """Base class for SDK errors."""


class ValidationError(Fact0Error):
    """Invalid event fields or types."""


class TransportError(Fact0Error):
    """HTTP failure or unreachable server."""

    def __init__(self, message: str, *, status_code: int | None = None):
        super().__init__(message)
        self.status_code = status_code
