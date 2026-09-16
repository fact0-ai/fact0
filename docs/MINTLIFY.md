# Publishing the documentation with Mintlify

This file is for documentation maintainers. Running a self-hosted Fact0 installation does not require a Mintlify account or a documentation deployment.

The site source is the `docs/` directory of [fact0-ai/fact0](https://github.com/fact0-ai/fact0). In the Mintlify dashboard, connect that repository, select the intended release branch, enable monorepo mode, and set the documentation directory to `docs` (where `docs.json` lives). The maintained public documentation domain is `docs.fact0.io`.

## Preview and validate

Use Node 22 and run from the repository root:

```sh
bash scripts/sync-openapi.sh
python3 docs/scripts/validate-docs.py
cd docs
npx mintlify dev --port 3001
```

The CLI prints the preview URL. Install PyYAML for the validator if needed. Port 3001 avoids the application dashboard's default port.

The canonical REST specifications are `openapi/audit.v1.yaml` and `openapi/telemetry.v1.yaml` at the repository root. The sync script copies them into `docs/openapi/`, which is referenced by `docs.json`. There is no separate application checkout to locate.

The repository workflows validate documentation paths, JSON, YAML and generated references. Check links and the local preview before publishing a documentation update. Publishing documentation does not deploy the Fact0 application or create owner accounts.
