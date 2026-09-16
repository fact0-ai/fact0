#!/usr/bin/env python3
"""A deterministic Fact0 execution and audit example. No model account required."""
import json
import os
from pathlib import Path
import uuid

from fact0 import Client


def main():
    base_url = os.environ.get("FACT0_BASE_URL", "http://localhost:8000")
    api_key = os.environ.get("FACT0_API_KEY")
    if not api_key:
        raise SystemExit("Set FACT0_API_KEY to a key created in your local dashboard.")
    client = Client(api_key=api_key, base_url=base_url, sync=True, raise_on_error=True)
    run_id = str(uuid.uuid4())
    try:
        with client.telemetry.execution(agent_id="local-python", agent_name="Local Python example", trigger="manual", metadata={"example": "local-python", "run_id": run_id}) as execution:
            with execution.span("Calculate a total", span_type="CUSTOM") as parent:
                with execution.span("add", span_type="TOOL_CALL", parent_span_id=parent.id) as tool:
                    tool.complete(tool_call={"tool_name": "add", "input": {"inline": {"a": 20, "b": 22}, "content_type": "application/json"}, "output": {"inline": {"result": 42}, "content_type": "application/json"}})
                with execution.span("Handled failure", span_type="CUSTOM", parent_span_id=parent.id) as failed:
                    failed.complete(status="FAILED", error={"code": "EXAMPLE_ERROR", "message": "A synthetic failure for inspection"})
                parent.complete(output={"result": 42})
        client.flush()
        # Read back the result so a fail-soft client cannot report false success.
        stored = client.telemetry.get_execution(execution.id)
        spans_response = client.telemetry.get_spans(execution.id)
        spans = spans_response.get("spans", []) if isinstance(spans_response, dict) else spans_response
        if len(spans) != 3 or any(s.get("status") == "RUNNING" for s in spans):
            raise RuntimeError("Example did not persist all three completed/failed spans")
        if stored.get("status") != "COMPLETED":
            raise RuntimeError("Example execution did not finish")
        client.audit.log(actor={"id": "local-python", "type": "agent"}, action="example.completed", resource={"id": execution.id, "type": "execution"}, outcome="success", metadata={"run_id": run_id, "result": 42})
        client.audit.flush()
        verification = client.audit.verify()
        if not verification.get("valid"):
            raise RuntimeError(f"Chain verification failed: {verification}")
        output = Path(os.environ.get("FACT0_EXAMPLE_OUTPUT", ".release-local/python-evidence.zip"))
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_bytes(client.audit.export_evidence_pack())
        print(json.dumps({"execution_id": execution.id, "spans": len(spans), "chain_valid": True, "evidence_pack": str(output)}, indent=2))
    finally:
        client.close()


if __name__ == "__main__":
    main()
