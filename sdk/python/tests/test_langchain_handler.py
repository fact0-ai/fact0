"""Tests for the rewritten LangChain/LangGraph callback handler.

Uses real langchain_core (fake chat model + real CallbackManager wiring) so we
verify the handler actually works with LangChain, not just duck-typed calls.
"""

from __future__ import annotations

import pytest

pytest.importorskip("langchain_core")

from langchain_core.callbacks.base import BaseCallbackHandler
from langchain_core.language_models.fake_chat_models import FakeListChatModel
from langchain_core.prompts import ChatPromptTemplate

from fact0 import Client
from fact0.integrations.langchain import Fact0CallbackHandler


def _make_client(mock_server) -> Client:
    def responder(req):
        if req["path"] == "/api/v1/executions" and req["method"] == "POST":
            return 200, b'{"id": "exec_lc_1"}'
        return 200, b'{"accepted": 1, "rejected": 0}'

    mock_server.set_responder(responder)
    return Client(api_key="f0_live_test", base_url=mock_server.url, sync=True)


def _span_requests(mock_server):
    return [
        span
        for req in mock_server.received
        if req["path"].endswith("/spans") and req["method"] == "POST"
        for span in req["json"]["spans"]
    ]


def test_is_real_base_callback_handler() -> None:
    assert issubclass(Fact0CallbackHandler, BaseCallbackHandler)
    # Attributes LangChain's CallbackManager reads off every handler.
    handler = Fact0CallbackHandler.__new__(Fact0CallbackHandler)
    for attr in ("ignore_llm", "ignore_chain", "ignore_retriever", "raise_error", "run_inline"):
        assert hasattr(handler, attr)


def test_real_chain_invoke_creates_execution_spans_and_io(mock_server) -> None:
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(
        client,
        agent_id="test-agent",
        session_id="sess-42",
        user_id="user-7",
    )

    llm = FakeListChatModel(responses=["Paris is the capital of France."])
    prompt = ChatPromptTemplate.from_messages(
        [("system", "You are a geographer."), ("user", "Capital of {country}?")]
    )
    chain = prompt | llm

    result = chain.invoke({"country": "France"}, config={"callbacks": [handler]})
    assert "Paris" in result.content

    client.close()

    # Execution started with session/user metadata and ended COMPLETED.
    exec_reqs = [r for r in mock_server.received if r["path"] == "/api/v1/executions" and r["method"] == "POST"]
    assert len(exec_reqs) == 1
    assert exec_reqs[0]["json"]["metadata"]["session_id"] == "sess-42"
    assert exec_reqs[0]["json"]["metadata"]["user_id"] == "user-7"

    end_reqs = [r for r in mock_server.received if r["path"].endswith("/end")]
    assert len(end_reqs) == 1
    assert end_reqs[0]["json"]["status"] == "COMPLETED"

    spans = _span_requests(mock_server)
    completed = {s["id"]: s for s in spans if s.get("status") in ("COMPLETED", "FAILED")}

    # The chat-model span nests under the root chain span.
    llm_spans = [s for s in completed.values() if s["span_type"] == "MODEL_INVOCATION"]
    assert len(llm_spans) == 1
    llm_span = llm_spans[0]
    assert llm_span.get("parent_span_id"), "LLM span must be linked to a parent span"

    root_spans = [s for s in completed.values() if "parent_span_id" not in s]
    assert len(root_spans) == 1, "exactly one root span per invocation"

    # Prompt captured as chat messages; completion captured as a message.
    mi = llm_span["model_invocation"]
    prompt_messages = mi["prompt"]["inline"]["messages"]
    assert prompt_messages[0]["role"] == "system"
    assert prompt_messages[1]["role"] == "user"
    assert "France" in prompt_messages[1]["content"]
    completion_messages = mi["completion"]["inline"]["messages"]
    assert completion_messages[0]["role"] == "assistant"
    assert "Paris" in completion_messages[0]["content"]
    assert mi["session_id"] == "sess-42"
    assert isinstance(mi["latency_ms"], int)


def test_two_invocations_create_two_executions(mock_server) -> None:
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(client, agent_id="test-agent")

    llm = FakeListChatModel(responses=["one", "two"])
    chain = ChatPromptTemplate.from_messages([("user", "{q}")]) | llm

    chain.invoke({"q": "a"}, config={"callbacks": [handler]})
    chain.invoke({"q": "b"}, config={"callbacks": [handler]})
    client.close()

    exec_starts = [r for r in mock_server.received if r["path"] == "/api/v1/executions" and r["method"] == "POST"]
    end_reqs = [r for r in mock_server.received if r["path"].endswith("/end")]
    assert len(exec_starts) == 2
    assert len(end_reqs) == 2


def test_bare_llm_invoke_ends_execution(mock_server) -> None:
    """A bare llm.invoke() (LLM run as root) must not leave the execution RUNNING."""
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(client, agent_id="test-agent")

    llm = FakeListChatModel(responses=["hi"])
    llm.invoke("hello", config={"callbacks": [handler]})
    client.close()

    end_reqs = [r for r in mock_server.received if r["path"].endswith("/end")]
    assert len(end_reqs) == 1
    assert end_reqs[0]["json"]["status"] == "COMPLETED"


def test_chain_error_marks_execution_failed(mock_server) -> None:
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(client, agent_id="test-agent")

    def boom(_: dict) -> str:
        raise ValueError("kaput")

    from langchain_core.runnables import RunnableLambda

    with pytest.raises(ValueError):
        RunnableLambda(boom).invoke({}, config={"callbacks": [handler]})
    client.close()

    end_reqs = [r for r in mock_server.received if r["path"].endswith("/end")]
    assert len(end_reqs) == 1
    assert end_reqs[0]["json"]["status"] == "FAILED"

    spans = _span_requests(mock_server)
    failed = [s for s in spans if s.get("status") == "FAILED"]
    assert failed and failed[0]["error"]["code"] == "ValueError"
    assert "kaput" in failed[0]["error"]["message"]


def test_tool_calls_captured_with_io(mock_server) -> None:
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(client, agent_id="test-agent")

    from langchain_core.tools import tool

    @tool
    def add(a: int, b: int) -> int:
        """Add two numbers."""
        return a + b

    result = add.invoke({"a": 2, "b": 3}, config={"callbacks": [handler]})
    assert result == 5
    client.close()

    spans = _span_requests(mock_server)
    tool_spans = [s for s in spans if s["span_type"] == "TOOL_CALL" and s.get("tool_call")]
    assert len(tool_spans) == 1
    tc = tool_spans[0]["tool_call"]
    assert tc["tool_name"] == "add"
    assert tc["input"]["inline"] == {"a": 2, "b": 3}
    assert "5" in str(tc["output"]["inline"])

    # Tool as root run also ends its execution.
    end_reqs = [r for r in mock_server.received if r["path"].endswith("/end")]
    assert len(end_reqs) == 1


def test_per_invoke_session_metadata_override(mock_server) -> None:
    client = _make_client(mock_server)
    handler = Fact0CallbackHandler(client, agent_id="test-agent", session_id="default-sess")

    llm = FakeListChatModel(responses=["ok"])
    chain = ChatPromptTemplate.from_messages([("user", "{q}")]) | llm
    chain.invoke(
        {"q": "hi"},
        config={"callbacks": [handler], "metadata": {"fact0_session_id": "sess-override"}},
    )
    client.close()

    exec_req = next(r for r in mock_server.received if r["path"] == "/api/v1/executions" and r["method"] == "POST")
    assert exec_req["json"]["metadata"]["session_id"] == "sess-override"
