# Build the public website

The public landing and legal pages can be exported as static files. They use the same components, styles, local fonts and assets as the self-hosted application. The public build sends visitors to the repository quickstart and guides.

Use Python 3 and Node 22 or newer. Install the locked web dependencies once, then run from the repository root:

```sh
npm --prefix web ci
python3 scripts/build-marketing.py
```

The result is `.release-local/launch/marketing/`, ignored by Git. The builder stages an explicit allowlist in a temporary directory, reuses `web/node_modules`, builds a Next.js static export, checks the result, and removes temporary build files. Re-running replaces only an output directory marked as an earlier generated marketing export. An unrelated existing output directory is refused. A failed build keeps the previous export.

Optional settings:

```sh
python3 scripts/build-marketing.py --origin https://fact0.io --output /tmp/fact0-marketing
```

`--origin` sets canonical, Open Graph and sitemap URLs. `--docs-url https://docs.example.com` selects a published documentation site only when it matches the release; leaving it empty uses the GitHub instructions. These values are fixed during the build. The script does not load `.env` files or inherit application/provider configuration.

The output contains `/`, `/legal/`, the existing legal aliases, a static `404.html`, robots/sitemap files, favicon/brand assets and a `licenses/` directory. Keep that directory with the deployment: it contains the application/font licenses and generated browser dependency notices, including Next.js's vendored licenses. The export contains no dashboard, sign-in, auth/API proxy, database connection or server function. A build flag changes “Open dashboard” to “Run locally” only in this export; the ordinary self-hosted web build retains its dashboard link and server behavior.

Serve the generated directory with a static host that supports directory indexes and uses `404.html` for missing pages. Local preview:

```sh
python3 -m http.server 3100 --directory .release-local/launch/marketing
```

The Python preview serves files and directory indexes; the production host must configure its own HTTP 404 handling. Verify desktop/mobile rendering, legal pages, local assets and external guide links before promotion. Deployment credentials, provider project IDs, custom-domain bindings and redirects belong in the operator's deployment configuration. This script builds files and does not deploy or modify a cloud project.
