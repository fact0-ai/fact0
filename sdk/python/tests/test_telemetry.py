"""Tests for the telemetry REST client."""

from __future__ import annotations

from fact0._http import SyncHTTP
from fact0.telemetry.client import TelemetryClient


def test_start_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    result = client.start_execution(
        agent_id="agent_1",
        agent_name="demo-agent",
        trigger="manual",
        metadata={"env": "test"},
    )

    assert result == {"accepted": 0, "rejected": 0, "ids": []}
    req = mock_server.received[0]
    assert req["method"] == "POST"
    assert req["path"] == "/api/v1/executions"
    assert req["json"]["agent_id"] == "agent_1"
    assert req["json"]["agent_name"] == "demo-agent"


def test_end_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    client.end_execution("exec_123", "success")
    client.flush()

    req = mock_server.received[0]
    assert req["method"] == "PUT"
    assert req["path"] == "/api/v1/executions/exec_123/end"
    assert req["json"]["status"] == "success"


def test_get_execution(mock_server) -> None:
    client = TelemetryClient(SyncHTTP(mock_server.url, "alk_live_test", max_retries=0))
    client.get_execution("exec_456")

    req = mock_server.received[0]
    assert req["method"] == "GET"
    assert req["path"] == "/api/v1/executions/exec_456"


def test_telemetry_auto_audit(mock_server) -> None:
    from fact0 import Client

    client = Client(
        api_key="f0_live_test",
        base_url=mock_server.url,
        sync=True,
    )

    def responder(req):
        if req["path"] == "/api/v1/executions" and req["method"] == "POST":
            return 200, b'{"id": "exec_auto_123"}'
        return 200, b'{"receipt_id": "r_123", "status": "committed"}'

    mock_server.set_responder(responder)

    with client.telemetry.execution(agent_id="my_agent") as exec_ctx:
        with exec_ctx.span("my_model_span", span_type="MODEL_INVOCATION") as span:
            span.complete(
                model_invocation={
                    "model_name": "gpt-4o",
                    "total_tokens": 150,
                }
            )

    client.close()

    paths = [req["path"] for req in mock_server.received]
    assert "/api/v1/executions" in paths
    assert "/v1/events/batch" in paths

    audit_req = next(r for r in mock_server.received if r["path"] == "/v1/events/batch")
    event = audit_req["json"]["events"][0]
    assert event["actor"]["id"] == "my_agent"
    assert event["action"] == "agent.model.invoke"
    assert event["metadata"]["execution_id"] == "exec_auto_123"
    assert event["metadata"]["model"] == "gpt-4o"


def test_async_telemetry_flow(mock_server) -> None:
    import asyncio
    from fact0 import AsyncClient

    def responder(req):
        if req["path"] == "/api/v1/executions" and req["method"] == "POST":
            return 200, b'{"id": "exec_async_123"}'
        return 200, b'{"receipt_id": "r_123", "status": "committed"}'

    mock_server.set_responder(responder)

    async def run() -> None:
        async with AsyncClient(
            api_key="f0_live_test",
            base_url=mock_server.url,
            sync=True,
        ) as client:
            async with client.telemetry.execution(agent_id="async_agent") as exec_ctx:
                assert exec_ctx.id == "exec_async_123"
                async with exec_ctx.span("async_span", span_type="TOOL_CALL") as span:
                    await span.log_event("step_1", {"status": "ok"})
                    await span.complete(tool_call={"tool_name": "calc"})

    asyncio.run(run())

    paths = [req["path"] for req in mock_server.received]
    assert "/api/v1/executions/exec_async_123/spans" in paths
    assert "/api/v1/executions/exec_async_123/events" in paths
    assert "/api/v1/executions/exec_async_123/end" in paths


def test_langchain_callback_metadata(mock_server) -> None:
    from fact0 import Client
    from fact0.integrations.langchain import Fact0CallbackHandler

    client = Client(
        api_key="f0_live_test",
        base_url=mock_server.url,
        sync=True,
    )

    def responder(req):
        if req["path"] == "/api/v1/executions" and req["method"] == "POST":
            return 200, b'{"id": "exec_lc_123"}'
        return 200, b'{"receipt_id": "r_123", "status": "committed"}'

    mock_server.set_responder(responder)

    handler = Fact0CallbackHandler(client=client, agent_id="lc_agent")

    handler.on_llm_start(
        serialized={"name": "test-gpt"},
        prompts=["tell me a joke"],
        run_id="run_1",
        metadata={
            "session_id": "sess_joke",
            "turn_sequence": 1,
            "prompt_name": "joke-generator",
            "prompt_version": 2,
            "cost_usd": 0.0015,
        }
    )

    class MockLLMResult:
        def __init__(self):
            self.llm_output = {"model_name": "gpt-4o", "token_usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}
            self.generations = [[type("Gen", (object,), {"text": "Why did the chicken cross the road?"})()]]

    handler.on_llm_end(
        response=MockLLMResult(),
        run_id="run_1",
    )

    client.close()

    span_req = next(r for r in mock_server.received if "spans" in r["path"])
    span = span_req["json"]["spans"][0]
    assert span["model_invocation"]["model_name"] == "gpt-4o"
    assert span["model_invocation"]["session_id"] == "sess_joke"
    assert span["model_invocation"]["turn_sequence"] == 1
    assert span["model_invocation"]["prompt_name"] == "joke-generator"
    assert span["model_invocation"]["prompt_version"] == 2
    assert span["model_invocation"]["cost_usd"] == 0.0015

