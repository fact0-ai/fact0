# ADR-004: Immutable history and current projections

Status: Updated for the fresh-install OSS release.

PostgreSQL triggers reject UPDATE and DELETE on `audit_events` and `execution_events`. OSS mode exposes no re-anchor or deletion/retention routes or workers. Audit hashes bind all immutable public event fields, including their tenant and allocated sequence number.

Executions and spans are current projections. A RUNNING span may move to one terminal state; its identity and start time remain fixed. The projection and immutable initial/terminal lifecycle snapshots commit in one transaction. Exact retries are accepted without adding history. Conflicting terminal content is rejected.

The database owner can still alter triggers, replace tables, or replace a full history; these controls do not provide an external trust anchor. Disk usage grows until the operator manages their installation. This release provides no automatic retention policy.
