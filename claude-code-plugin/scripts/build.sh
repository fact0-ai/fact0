#!/usr/bin/env bash
#
# build.sh — compile the Fact0 Claude Code collector binary (fact0-cc).
#
# Produces a self-contained Go static binary at bin/fact0-cc that the
# plugin's hooks invoke once per Claude Code lifecycle event.
# Paths resolve relative to this script, so it works from any checkout.
#
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The dev binary sits next to the checked-in bin/fact0-cc bootstrap script,
# which prefers it over downloading a release binary.
cd "${PLUGIN_DIR}/collector"
go build -o "${PLUGIN_DIR}/bin/fact0-cc-dev" .

echo "Build succeeded: ${PLUGIN_DIR}/bin/fact0-cc-dev"
