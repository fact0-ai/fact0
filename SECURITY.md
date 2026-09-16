# Security

This is an experimental, self-hosted release with best-effort maintenance and no security-response SLA. Only the latest released core is considered for fixes.

Use GitHub private vulnerability reporting for this repository to report a security issue. If private reporting is unavailable, open an issue requesting a private contact without including exploit details, credentials, prompts or other sensitive data.

The default installation binds to localhost and requires owner login/API keys. Full Claude content capture stores potentially sensitive prompts, source and tool output. Use a reduced capture mode where appropriate. Protect the local retry spool, database, backups and `.env`; configure TLS before exposing the installation remotely.

A valid audit hash chain establishes consistency of the records checked under its documented recipe. It does not establish complete capture, trustworthy agents, or resistance to an administrator replacing the database and signing keys. Export signatures must be checked against a public key obtained through a trusted, separate channel.
