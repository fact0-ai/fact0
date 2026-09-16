import { authClient } from "./auth-client";

// Module-level token cache - shared across all hooks in this tab.
// Tokens are valid for 15 minutes; we cache for 14 to leave a 60s
// safety margin. Inflight deduplication ensures only one /api/auth/token
// request fires even when multiple SWR hooks mount simultaneously.
let _tokenCache: { value: string; expiresAt: number } | null = null;
let _tokenGeneration = 0;
let _tokenInflight: Promise<string | null> | null = null;

export function invalidateTokenCache() {
  _tokenGeneration += 1;
  _tokenCache = null;
  _tokenInflight = null;
}

export async function fetchToken(): Promise<string | null> {
  const now = Date.now();
  if (_tokenCache && _tokenCache.expiresAt > now) {
    return _tokenCache.value;
  }
  if (_tokenInflight) return _tokenInflight;

  const generation = _tokenGeneration;
  _tokenInflight = authClient
    .token()
    .then(({ data }) => {
      if (generation !== _tokenGeneration) return fetchToken();
      const token = data?.token ?? null;
      if (token) {
        _tokenCache = { value: token, expiresAt: now + 14 * 60 * 1000 };
      }
      _tokenInflight = null;
      return token;
    })
    .catch(() => {
      if (generation !== _tokenGeneration) return fetchToken();
      _tokenInflight = null;
      return null;
    });

  return _tokenInflight;
}
