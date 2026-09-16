# ADR-003: Retry identity

Status: Updated for the fresh-install OSS release.

A start request with `idempotency_key` derives a stable execution ID from its tenant, agent, and key. Retrying returns the stored execution and its original start time. Without a key, a new execution is created.

Clients may provide span and event IDs. Implicit span IDs depend on execution, parent, name, kind, and the microsecond start timestamp, independent of batch position. Clients must provide distinct explicit IDs for otherwise identical concurrent work. Implicit event IDs derive from normalized event content.

Native IDs use 128 bits of SHA-256. Lifecycle IDs remain deterministic per execution, span, and transition. A duplicate event ID never rewrites history. A completed span cannot be replaced; retrying its original start or completion is safe. RUNNING parents must arrive before their children.
