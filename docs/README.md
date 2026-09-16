# Fact0 documentation

Documentation sources for the experimental self-hosted Fact0 core. The published reference is [docs.fact0.io](https://docs.fact0.io); all application, SDK, collector and documentation sources live in this repository.

To run Fact0 itself, follow the repository [quickstart](../README.md#quickstart). Mintlify is only the documentation renderer and is not an application dependency.

## Local documentation preview

Use Node 22 (`docs/.nvmrc`). From the repository root:

```sh
cd docs
npx mintlify dev --port 3001
```

Open the URL printed by the CLI. Port 3001 keeps the documentation preview separate from the local dashboard on port 3000. The initial CLI installation needs network access. With nvm, run `nvm use` inside `docs` first.

## API reference

The root `openapi/` directory is canonical. Copy its specifications into the documentation with:

```sh
# From the repository root:
bash scripts/sync-openapi.sh
python3 docs/scripts/validate-docs.py
```

Install PyYAML if the validator requests it. The compatibility scripts `docs/sync-openapi.sh` and `scripts/sync-openapi-from-app.sh` call the same local sync command; no sibling/private checkout or `APP_REPO` setting is used.

## Maintenance

The core and documentation workflows validate the sources. Keep examples aligned with the checked-in SDKs and the supported single-owner profile. Start with [Assistant.md](./Assistant.md) for terminology and [EXPLAINER.md](./EXPLAINER.md) for product boundaries.

Publishing the documentation is optional maintainer work; see [MINTLIFY.md](./MINTLIFY.md).
