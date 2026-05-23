# Fact0 documentation (Mintlify)

Product docs for [Fact0](https://fact0.io), deployed at [docs.fact0.io](https://docs.fact0.io).

This folder is part of the [fact0-ai/fact0](https://github.com/fact0-ai/fact0) monorepo (SDKs + docs).

## Mintlify deployment

| Setting | Value |
|---------|-------|
| Repository | `fact0-ai/fact0` |
| Branch | `main` |
| Monorepo path | **`docs`** |

See [MINTLIFY.md](./MINTLIFY.md) for dashboard setup and migration from `iyashjayesh/fact0-docs`.

## Local preview

Mintlify requires **Node LTS** (20 or 22). Homebrew’s default `node` may be 25+, which Mintlify rejects.

```bash
cd docs

# Homebrew: use node@22 for this shell (already installed on most dev machines)
export PATH="/opt/homebrew/opt/node@22/bin:$PATH"

npx mintlify dev
```

With [nvm](https://github.com/nvm-sh/nvm): `nvm use` (reads `.nvmrc` → Node 22).

## OpenAPI

API reference pages use `openapi/*.yaml`. Sync from the app monorepo (canonical source):

```bash
# from docs/ (while previewing Mintlify)
bash sync-openapi.sh

# or from fact0-ai/fact0 repo root
bash scripts/sync-openapi-from-app.sh
```

Auto-detects the app repo at `../` (nested clone) or `../fact0` (sibling clone).
Override with `APP_REPO=/path/to/iyashjayesh/fact0` if needed.

## CI

Validation runs via `.github/workflows/docs-validate.yml` at the repo root.
