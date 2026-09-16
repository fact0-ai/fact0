# Working on Fact0

This repository contains the experimental self-hosted Fact0 core, dashboard, SDKs, Claude Code plugin and documentation. License: MIT; preserve third-party notices.

## Architecture

- `api/`: Go 1.25 module; run Go commands from this directory.
- `web/`: Next.js 16 / React 19. Read the installed Next.js guide relevant to a change before editing app code.
- `sdk/python`, `sdk/typescript`, `sdk/go`: retain package identities and established tag conventions.
- `claude-code-plugin/collector`: separate Go module using `../../sdk/go` locally.
- `openapi/`: canonical public wire contract. `make docs-sync` updates documentation copies.

Audit and execution history are append-only. Represent lifecycle changes as new execution events and update only current projections. Never rewrite a historical audit chain to make a verification failure disappear. Bind all reads and writes to authenticated tenant/execution ownership.

## Supported release

One password-authenticated owner/workspace, fresh installations, synchronous ingestion, Python and Claude Code capture/inspection. Full Claude capture is the documented default; report omissions explicitly. No billing, hosted administration, invitations, Spotlight, external alerts/sharing, policy enforcement or OTLP routes. No background retention or external pricing/analytics calls.

## Checks

- API: `go vet ./...` and `go test -race ./...` from `api/`, with an isolated migrated PostgreSQL database in `FACT0_POSTGRES_DSN`.
- Web: `npm ci`, `npm run lint`, `npx tsc --noEmit`, `npm run build` from `web/`.
- Python: install `sdk/python[dev,langchain]`; `python -m pytest sdk/python/tests`.
- Go SDK and collector: `go test -race ./...` in each module.
- TypeScript SDK: `npm ci`, `npm test`, `npm run build`, `npm run typecheck`.
- Docs: `make docs-sync`, `python3 docs/scripts/validate-docs.py`.

Do not commit credentials, operator data, `.env`, dependencies or build output. Release tags for core use `core/v*`; SDK/plugin tag conventions remain separate. Fresh setup must refuse unknown nonempty databases. Production deployment credentials and private application history do not belong in this repository.
