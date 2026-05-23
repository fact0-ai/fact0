#!/usr/bin/env bash
# Convenience wrapper when working inside docs/.
exec bash "$(cd "$(dirname "$0")/.." && pwd)/scripts/sync-openapi-from-app.sh" "$@"
