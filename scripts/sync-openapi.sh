#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$repo/docs/openapi"
cp "$repo/openapi/audit.v1.yaml" "$repo/docs/openapi/audit.v1.yaml"
cp "$repo/openapi/telemetry.v1.yaml" "$repo/docs/openapi/telemetry.v1.yaml"
printf '%s\n' 'Updated docs/openapi from the canonical root openapi directory.'
