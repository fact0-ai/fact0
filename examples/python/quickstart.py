"""Minimal end-to-end quickstart.

Pre-requisites:
    1. Run the Fact0 backend (``make up && make dev``).
    2. Sign in at http://localhost:3000/dashboard and complete onboarding,
       or set FACT0_API_KEY from Settings → API Keys.
"""

from __future__ import annotations

import os

import fact0


def main() -> None:
    client = fact0.Client(
        api_key=os.environ["FACT0_API_KEY"],
    )

    client.audit.log(
        actor={"id": "user_123", "type": "human", "email": "admin@acme.com"},
        action="document.delete",
        resource={"id": "doc_456", "type": "document", "name": "Q3 Report"},
        outcome="success",
        metadata={"ip": "203.0.113.5", "reason": "user requested"},
    )

    client.close()
    print("logged 1 event")


if __name__ == "__main__":
    main()
