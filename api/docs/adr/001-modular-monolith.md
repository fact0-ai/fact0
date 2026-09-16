# ADR-001: Modular Monolith Architecture

**Status:** Accepted
**Date:** 2025-05-15

## Context

We need to choose a deployment architecture for Fact0's backend. Options: microservices, modular monolith, or simple monolith.

## Decision

Use a **modular monolith** - a single binary with clear internal module boundaries enforced via Go package structure.

## Rationale

- **Single deploy unit** reduces operational complexity during MVP.
- Package-level boundaries (`ingestion`, `lineage`, `query`, `replay`) enforce separation without network hops.
- Services communicate via direct function calls, not gRPC/HTTP internally - avoiding premature distributed system complexity.
- Migration to microservices is straightforward: extract packages into standalone services with gRPC interfaces.

## Consequences

- All modules share the same process and database connection pool.
- A failure in one module can affect others (acceptable for MVP).
- Module boundaries must be enforced through code review, not infrastructure.
