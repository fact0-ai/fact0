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

```bash
cd docs
npx mintlify dev
```

## OpenAPI

API reference pages use `openapi/*.yaml`. Sync from the app monorepo (canonical source):

```bash
# from fact0-ai/fact0 repo root
bash scripts/sync-openapi-from-app.sh
```

## CI

Validation runs via `.github/workflows/docs-validate.yml` at the repo root.
