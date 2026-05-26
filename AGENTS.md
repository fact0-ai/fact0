# AGENTS.md

Context for AI coding assistants working in the Fact0 SDK + docs repository.

## Project overview

**Fact0** provides audit logging and execution telemetry for AI agents. This repo contains **client SDKs** (Python, TypeScript, Go) and the **Mintlify documentation site**. The Go API server and Next.js dashboard live in the separate app monorepo: https://github.com/iyashjayesh/fact0

- **Docs (live):** https://docs.fact0.io
- **License:** Apache-2.0 / MIT (SDK packages MIT)

## Repository structure

| Directory | Description |
|-----------|-------------|
| `sdk/python/` | PyPI package `fact0-sdk` (`import fact0`) - audit + telemetry clients, LangChain/FastAPI integrations |
| `sdk/typescript/` | npm `@fact0/sdk` - audit + telemetry HTTP clients |
| `sdk/go/` | Go module `github.com/fact0-ai/fact0-go` |
| `docs/` | Mintlify site (`docs.json` at this path) - product + API reference |
| `openapi/` | Copied REST OpenAPI specs (canonical source: app repo `openapi/`) |
| `examples/` | Sample usage (expand over time) |
| `scripts/` | Repo utilities (`sync-openapi-from-app.sh`) |

**OpenAPI canonical source:** `iyashjayesh/fact0/openapi/` - never edit specs here without syncing from app repo first.

## Core APIs

### Python

```python
from fact0 import Client, AsyncClient

client = Client(api_key="alk_live_...")
client.audit.log(actor={...}, action="...", resource={...}, outcome="success")
client.telemetry.start_execution(...)
```

Optional: `from fact0.integrations.langchain import Fact0CallbackHandler`

### TypeScript

```typescript
import { Fact0Client } from "@fact0/sdk";

const client = new Fact0Client({ apiKey: process.env.FACT0_API_KEY! });
await client.audit.log({ ... });
```

Env: `FACT0_API_KEY`. API origin defaults to `https://api.fact0.io`; override via `base_url` / `baseUrl` / `BaseURL` in client config (local dev only).

### Go

```go
import fact0 "github.com/fact0-ai/fact0-go"

client := fact0.NewClient(fact0.Config{APIKey: os.Getenv("FACT0_API_KEY")})
err := client.Audit.Log(ctx, fact0.AuditEventInput{...})
```

## Development commands

### Python (`sdk/python/`)

```bash
pip install -e '.[dev]'
pytest -q
pip install -e '.[langchain]'   # optional integration
pip install -e '.[fastapi]'
```

### TypeScript (`sdk/typescript/`)

```bash
npm ci
npm run build    # tsup → dist/
npm test         # vitest
npm run typecheck
```

Do **not** commit `dist/` - CI builds before publish.

### Go (`sdk/go/`)

```bash
go vet ./...
go test -race -count=1 ./...
```

### Docs (`docs/`)

```bash
npx mintlify dev
bash scripts/generate-llms-full.sh   # from docs/ directory
```

Mintlify monorepo: connect this GitHub repo, set docs root to **`docs/`**. See `docs/MINTLIFY.md`.

## CI/CD

| Workflow | Triggers | Action |
|----------|----------|--------|
| `sdk-python.yml` | `sdk/python/**`, tag `sdk/python/v*` | pytest → PyPI on tag |
| `sdk-typescript.yml` | `sdk/typescript/**`, tag `sdk/typescript/v*` | build + vitest → npm on tag |
| `sdk-go.yml` | `sdk/go/**`, tag `sdk/go/v*` | vet + test (no registry publish) |
| `docs-validate.yml` | `docs/**` | OpenAPI parse, docs.json nav check, llms-full generation |

### Release tags

| Tag | Publishes |
|-----|-----------|
| `sdk/python/v1.0.1` | PyPI `fact0` |
| `sdk/typescript/v1.0.1` | npm `@fact0/sdk` |
| `sdk/go/v1.0.1` | Git tag for `go get` |

Secrets required: `PYPI_TOKEN`, `NPM_TOKEN`.

## Contributing

1. Sync OpenAPI before docs/API reference changes: `bash scripts/sync-openapi-from-app.sh`
2. Run tests for every SDK package you touch.
3. Update Mintlify MDX under `docs/` for user-facing doc changes.
4. New `.mdx` pages must appear in `docs/docs.json` navigation.

## Do NOT

- Commit `node_modules/`, `dist/`, `.venv/`, or secrets.
- Treat `openapi/` or `docs/openapi/` as canonical - sync from app monorepo.
- Modify CI publish workflows without coordinating PyPI/npm org access.
