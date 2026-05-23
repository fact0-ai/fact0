# Mintlify setup (docs.fact0.io)

This site lives in the **`docs/`** folder of the [fact0-ai/fact0](https://github.com/fact0-ai/fact0) monorepo.

## Connect GitHub

1. Open [Mintlify dashboard](https://dashboard.mintlify.com).
2. Add or edit the Fact0 project.
3. Connect repository: **`fact0-ai/fact0`**.
4. Enable **monorepo** mode and set **docs directory** to **`docs`** (where `docs.json` lives).
5. Production domain: **`docs.fact0.io`** (unchanged from prior `fact0-docs` repo).

## Local preview

```bash
cd docs
npx mintlify dev
```

Open http://localhost:3000

## OpenAPI reference

Specs are copied from the app monorepo:

```bash
# from repo root
bash scripts/sync-openapi-from-app.sh
```

Paths in `docs.json` are relative to `docs/`:

- `openapi/audit.v1.yaml`
- `openapi/telemetry.v1.yaml`

## CI validation

`.github/workflows/docs-validate.yml` runs on every PR touching `docs/**`.

## Migration from iyashjayesh/fact0-docs

After verifying preview deploys from this repo, disconnect the old `iyashjayesh/fact0-docs` GitHub integration in Mintlify and archive that repository.
