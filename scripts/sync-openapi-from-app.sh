#!/usr/bin/env bash
# Compatibility wrapper; all source now lives in this repository.
exec bash "$(cd "$(dirname "$0")" && pwd)/sync-openapi.sh" "$@"
