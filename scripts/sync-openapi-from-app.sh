#!/usr/bin/env bash
# Copy canonical OpenAPI specs from the app monorepo into docs/openapi/ and openapi/.
#
# Usage (from org repo root or docs/):
#   bash scripts/sync-openapi-from-app.sh
#   cd docs && bash sync-openapi.sh
#   APP_REPO=/path/to/iyashjayesh/fact0 bash scripts/sync-openapi-from-app.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

resolve_app_repo() {
  if [[ -n "${APP_REPO:-}" ]]; then
    if [[ -d "$APP_REPO/openapi" ]]; then
      echo "$(cd "$APP_REPO" && pwd)"
      return 0
    fi
    echo "APP_REPO=$APP_REPO has no openapi/ directory" >&2
    return 1
  fi

  local candidate
  for candidate in \
    "$ROOT/.." \
    "$ROOT/../fact0" \
    "$ROOT/../../fact0"; do
    if [[ -d "$candidate/openapi" ]]; then
      echo "$(cd "$candidate" && pwd)"
      return 0
    fi
  done
  return 1
}

APP_REPO="$(resolve_app_repo)" || {
  echo "App monorepo not found (expected openapi/ with *.yaml)." >&2
  echo "Set APP_REPO=/path/to/fact0 (iyashjayesh/fact0 clone)" >&2
  echo "Tried: $ROOT/.. and $ROOT/../fact0" >&2
  exit 1
}

mkdir -p "$ROOT/openapi" "$ROOT/docs/openapi"
cp "$APP_REPO/openapi/"*.yaml "$ROOT/openapi/"
cp "$APP_REPO/openapi/"*.yaml "$ROOT/docs/openapi/"
echo "Synced openapi/*.yaml from $APP_REPO → openapi/ and docs/openapi/"
