#!/usr/bin/env bash
# Acceptance test for a disposable, freshly initialized Compose installation.
set -euo pipefail
umask 077
repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
python3 -m venv .release-local/smoke-venv
.release-local/smoke-venv/bin/pip install -q -e ./sdk/python cryptography
.release-local/smoke-venv/bin/python scripts/compose-smoke.py
.release-local/smoke-venv/bin/python scripts/restore-smoke.py
