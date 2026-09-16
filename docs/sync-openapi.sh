#!/usr/bin/env bash
exec bash "$(cd "$(dirname "$0")/.." && pwd)/scripts/sync-openapi.sh" "$@"
