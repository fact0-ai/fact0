#!/usr/bin/env bash
#
# install-local.sh — build the collector and print the steps to install the
# fact0-claude-code plugin locally for testing.
#
# This script is intentionally safe: it does NOT use sudo, does NOT write to
# any global/system location, and only builds the binary into the plugin's
# own bin/ directory. The actual plugin install is performed by YOU inside
# Claude Code using the two slash commands printed below.
#
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${PLUGIN_DIR}/.." && pwd)"

echo "==> Building the collector binary (fact0-cc)..."
bash "${PLUGIN_DIR}/scripts/build.sh"

echo
echo "==> Build complete. Now finish the install from INSIDE Claude Code."
echo
echo "1) Register the repo root as a local plugin marketplace"
echo "   (the marketplace manifest lives at .claude-plugin/marketplace.json):"
echo
echo "     /plugin marketplace add ${REPO_ROOT}"
echo
echo "2) Install the plugin:"
echo
echo "     /plugin install fact0-claude-code@fact0"
echo
echo "==> Required environment (set before / while running Claude Code):"
echo
echo "     export FACT0_API_KEY=...            # required: your Fact0 API key"
echo "     export FACT0_BASE_URL=http://localhost:8000"
echo
echo "==> Done. Raw capture is the default. Restart Claude Code after setting the environment."
