#!/usr/bin/env bash
# Copy canonical OpenAPI specs from the app monorepo into docs/openapi/ and openapi/.
#
# Usage:
#   bash scripts/sync-openapi-from-app.sh
#   APP_REPO=/path/to/iyashjayesh/fact0 bash scripts/sync-openapi-from-app.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP_REPO="${APP_REPO:-$ROOT/../fact0}"

if [[ ! -d "$APP_REPO/openapi" ]]; then
  echo "App monorepo not found at $APP_REPO/openapi" >&2
  echo "Set APP_REPO=/path/to/fact0 (iyashjayesh/fact0 clone)" >&2
  exit 1
fi

mkdir -p "$ROOT/openapi" "$ROOT/docs/openapi"
cp "$APP_REPO/openapi/"*.yaml "$ROOT/openapi/"
cp "$APP_REPO/openapi/"*.yaml "$ROOT/docs/openapi/"
echo "Synced openapi/*.yaml from $APP_REPO → openapi/ and docs/openapi/"
