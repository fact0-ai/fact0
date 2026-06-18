# Fact0 SDKs and documentation

Official client SDKs and product docs for [Fact0](https://fact0.io) — the universal fact layer with tamper-evident audit trails and execution telemetry for AI agents.

| Resource | URL |
|----------|-----|
| Marketing | [fact0.io](https://fact0.io) |
| Dashboard | [app.fact0.io](https://app.fact0.io) |
| Docs | [docs.fact0.io](https://docs.fact0.io) |
| App monorepo (API + web) | [iyashjayesh/fact0](https://github.com/iyashjayesh/fact0) |

## Install

```bash
pip install fact0
npm install @fact0/sdk
go get github.com/fact0-ai/fact0/sdk/go
```

## Repository layout

```
sdk/python/       # pip: fact0
sdk/typescript/   # npm: @fact0/sdk
sdk/go/           # go get github.com/fact0-ai/fact0/sdk/go
docs/             # Mintlify site (docs.fact0.io)
openapi/          # Copy of REST contracts (canonical source: app monorepo)
```

See [AGENTS.md](./AGENTS.md) for development commands, CI, and release tags.

## Local development

```bash
# Python
cd sdk/python && pip install -e '.[dev]' && pytest -q

# TypeScript
cd sdk/typescript && npm ci && npm run build && npm test

# Go
cd sdk/go && go test ./...

# Docs
cd docs && npx mintlify dev
```

## OpenAPI sync

REST specs are authored in the [app monorepo](https://github.com/iyashjayesh/fact0) under `openapi/`. Sync copies into this repo:

```bash
bash scripts/sync-openapi-from-app.sh
# APP_REPO=/path/to/fact0 bash scripts/sync-openapi-from-app.sh
```

## Releases

| Tag prefix | Publishes |
|------------|-----------|
| `sdk/python/v*` | [PyPI `fact0`](https://pypi.org/project/fact0/) |
| `sdk/typescript/v*` | [npm `@fact0/sdk`](https://www.npmjs.com/package/@fact0/sdk) |
| `sdk/go/v*` | Git tag for Go modules |

## License

MIT - see [LICENSE](./LICENSE).
