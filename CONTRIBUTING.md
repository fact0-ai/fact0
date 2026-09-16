# Contributing

Fact0 is experimental and maintained on a best-effort basis. Contributions and reproducible bug reports are welcome; response times and new features are not promised.

Start with the root quickstart and AGENTS.md. Use synthetic fixtures and an isolated PostgreSQL database. Never include real prompts, API keys, customer data or private transcripts in an issue or test.

A pull request should explain the affected behavior, include regression coverage for substantive fixes, and update documentation when behavior changes. Preserve SDK package identities, explicit local API destinations and append-only history. Keep core scope focused on capture, inspection and verification.

Run checks for each affected component. API database tests require a migrated PostgreSQL instance; skipped database tests do not qualify as release validation. Both the deterministic Python walkthrough and collector-to-API workflow must pass for a core release.

The MIT license applies to contributions. Include notices for third-party assets and dependencies. Security reports follow SECURITY.md.
