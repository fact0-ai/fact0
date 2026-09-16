# ADR-005: DAG Over Tree for Span Relationships

**Status:** Accepted
**Date:** 2025-05-15

## Context

AI agent executions are not strictly tree-shaped. A span may depend on multiple prior spans (e.g., a synthesis step that requires outputs from two parallel tool calls).

## Decision

Model span relationships as a **directed acyclic graph (DAG)**, not a tree.

- `parent_span_id` provides the primary tree-like hierarchy.
- `caused_by_span_ids[]` captures additional causal dependencies.
- The `span_causality` table stores DAG edges separately from the `spans` table.

## Rationale

- AI agents commonly have fan-out/fan-in patterns (parallel tool calls → synthesis).
- Tree-only models force lossy representation of causal relationships.
- DAG reconstruction enables richer replay and debugging.
- Visualization with React Flow natively supports DAG layouts.

## Consequences

- DAG rendering is more complex than tree rendering.
- Cycle detection must be enforced at ingestion time (future work).
- Topological sorting is needed for deterministic replay ordering.
