# What Fact0 records

Fact0 is experimental self-hosted software for inspecting agent activity. A local owner can inspect Python executions and supported Claude Code sessions, expand captured payloads, replay recorded execution history, verify an audit chain and download signed exports.

## Two parallel records

| Record | What it contains | Main use |
| --- | --- | --- |
| Execution telemetry | Executions, parent/child spans, timestamps, events and structured model/tool details | Inspect a run's flow, timing, inputs, outputs and errors |
| Audit log | Actor, action, resource, outcome, metadata and preceding event hash | Search recorded actions and check stored history for inconsistencies |

The telemetry API lives under `/api/v1`; the audit API lives under `/v1`. Both require authentication. SDKs and collectors must use the operator's explicit destination. The local dashboard accesses these APIs through same-origin server proxies.

Python instrumentation can create execution spans and audit events. The Claude collector maps supported hook activity into both pipelines, with session IDs connecting related records. Neither pipeline automatically observes every possible agent action.

## A concrete workflow

The repository's `examples/local-python.py` creates a nested execution, records a successful tool and a handled failure, reads back the results, checks the audit chain and saves an evidence ZIP. It runs without a model-provider account.

In the dashboard, open Executions to inspect the span graph and timing waterfall. Replay steps through the stored events; it does not call the tools again. Audit logs support action/prefix, actor, resource, session, outcome and date filters. Verification and PDF/evidence ZIP exports cover the selected time range.

For Claude Code, prepare the collector, set the destination and API key, then install the plugin. Coding Agents groups captured prompts, tools, assistant text and errors by session. Expand an input, output or metadata field to inspect, copy or download the captured value. Partial or unavailable transcript capture includes a reason.

## What verification establishes

The audit chain links stored records using SHA-256. Verification recomputes record hashes and checks preceding links. Evidence exports contain signed files; an independently retained instance public key lets a reviewer verify their signatures.

These checks establish consistency with the stored chain or trusted key. They do not prove that an agent reported truthfully, that every action was captured, or that an administrator with database and signing-key access could not replace an entire history. Fact0 does not provide compliance certification or governance enforcement.

## Operating the experiment

The supported profile is a fresh installation with one owner and one workspace. Raw capture may contain sensitive data. The operator controls access, TLS, backups, storage and retention; records do not expire automatically in this profile. APIs and storage formats may change, and maintenance is best effort.

Start with the repository quickstart, then read the Python or Claude Code guide and the security and verification notes before capturing real content.
