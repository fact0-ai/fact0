#!/usr/bin/env python3
"""Print only the persistent public export key from this instance's .env."""
import base64
from pathlib import Path

env = Path(__file__).resolve().parents[1] / ".env"
for line in env.read_text().splitlines():
    if line.startswith("FACT0_SIGNING_KEY="):
        private_key = base64.b64decode(line.split("=", 1)[1], validate=True)
        if len(private_key) != 64:
            raise SystemExit("Invalid Ed25519 key length")
        print(base64.b64encode(private_key[32:]).decode())
        break
else:
    raise SystemExit("FACT0_SIGNING_KEY is missing")
