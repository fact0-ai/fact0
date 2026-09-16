# Fact0 web

Next.js 16 dashboard for the experimental single-owner edition. See the repository quickstart for Docker Compose setup.

Local development: copy `.env.example` to `.env.local`, use the same PostgreSQL database as the Go API, set a random 32+ character `BETTER_AUTH_SECRET`, apply migrations, and run `npm ci && npm run dev`.

No OAuth, email, payment, analytics or model-provider credentials are used. The owner is created interactively using `node scripts/owner.mjs create`, or non-interactively with `create --email owner@example.invalid --password-stdin`; run `reset-password` with the same email to reset it. Keep the web/API running during creation so the CLI can bootstrap the tenant and print the first API key. In local development, `npm run owner -- ...` loads `.env.local` automatically. Passwords are read from stdin or a masked terminal prompt, never command arguments.

Browser requests use same-origin `/v1/*` and `/api/v1/*` streaming proxies. `FACT0_BACKEND_URL` points to the private API; `BETTER_AUTH_URL` is the public installation origin and JWT issuer/audience. Cookies are host-only. The API should fetch JWKS from `/api/auth/jwks` through the private web service URL.

Validation: `npm run lint`, `npm run typecheck`, `npm test`, `npm run build`. Production builds do not require authentication secrets or a running database.
