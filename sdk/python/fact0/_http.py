"""Shared HTTP utilities with retries and auth."""

from __future__ import annotations

import logging
import os
import time
from typing import Any, Mapping

import requests

from .exceptions import TransportError

_log = logging.getLogger("fact0")

_RETRYABLE = {429, 500, 502, 503, 504}
DEFAULT_BASE_URL = "https://api.fact0.io"
DEFAULT_TIMEOUT_S = 30.0
USER_AGENT = "fact0-python/1.0.2"


def env_api_key() -> str | None:
    return os.environ.get("FACT0_API_KEY")


class SyncHTTP:
    """Synchronous HTTP client with bounded retries."""

    def __init__(
        self,
        base_url: str,
        api_key: str | None = None,
        *,
        timeout_s: float = DEFAULT_TIMEOUT_S,
        max_retries: int = 3,
        backoff_base_s: float = 0.2,
        sync_ingest: bool = False,
        session: requests.Session | None = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout_s = timeout_s
        self.max_retries = max_retries
        self.backoff_base_s = backoff_base_s
        self.sync_ingest = sync_ingest
        self.session = session or requests.Session()

    def _headers(self, *, auth: bool = True, sync: bool | None = None) -> dict[str, str]:
        headers = {
            "Content-Type": "application/json",
            "User-Agent": USER_AGENT,
        }
        if auth and self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        use_sync = self.sync_ingest if sync is None else sync
        if use_sync:
            headers["X-Fact0-Sync"] = "true"
        return headers

    def request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any | None = None,
        params: Mapping[str, Any] | None = None,
        auth: bool = True,
        sync: bool | None = None,
        expect_json: bool = True,
    ) -> Any:
        url = f"{self.base_url}{path}"
        last_err: Exception | None = None
        for attempt in range(self.max_retries + 1):
            try:
                resp = self.session.request(
                    method,
                    url,
                    json=json_body,
                    params=params,
                    headers=self._headers(auth=auth, sync=sync),
                    timeout=self.timeout_s,
                )
            except requests.RequestException as exc:
                last_err = exc
                _log.warning("network error (attempt %d): %s", attempt + 1, exc)
            else:
                retry_after = resp.headers.get("Retry-After")
                if resp.status_code < 300:
                    if not expect_json:
                        return resp.content
                    try:
                        return resp.json()
                    except ValueError:
                        return {}
                err_msg = resp.text if resp.text else "(no response body)"
                if resp.status_code not in _RETRYABLE:
                    raise TransportError(
                        f"{method} {path} returned {resp.status_code}: {err_msg}",
                        status_code=resp.status_code,
                    )
                last_err = TransportError(
                    f"{method} {path} returned {resp.status_code}: {err_msg}",
                    status_code=resp.status_code,
                )
                if retry_after:
                    try:
                        time.sleep(float(retry_after))
                        continue
                    except ValueError:
                        pass

            if attempt < self.max_retries:
                time.sleep(self.backoff_base_s * (2**attempt))

        raise TransportError(f"giving up after {self.max_retries + 1} attempts: {last_err}")
