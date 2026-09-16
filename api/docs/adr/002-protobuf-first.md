# ADR-002: REST contracts for the core release

Status: Supersedes the early protobuf-first proposal for OSS v1.

The supported ingestion and inspection surfaces are JSON REST endpoints documented in the repository's `openapi/` directory. Go domain types, SDK types, and OpenAPI definitions must evolve together. CI checks the documented route registrations.

OTLP HTTP/gRPC receivers are excluded from the supported OSS profile. The retained Python example and Claude Code collector use native REST ingestion.
