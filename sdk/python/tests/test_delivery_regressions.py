"""Client barriers and HTTP-200 item errors must not imply successful delivery."""
import threading

import pytest

from fact0 import Client
from fact0.exceptions import TransportError


def event():
    return dict(actor={"id": "test", "type": "agent"}, action="test.done",
                resource={"id": "test", "type": "test"}, outcome="success")


def test_flush_waits_for_an_already_sending_batch(mock_server):
    started = threading.Event()
    release = threading.Event()
    finished = threading.Event()

    def responder(_):
        started.set()
        assert release.wait(3)
        return 200, b'{"accepted":1,"rejected":0}'

    mock_server.set_responder(responder)
    client = Client(api_key="f0_live_fixture", base_url=mock_server.url,
                    batch_max_size=1, batch_max_wait_ms=1)
    waiter = None
    try:
        client.audit.log(**event())
        assert started.wait(3)
        waiter = threading.Thread(target=lambda: (client.audit.flush(), finished.set()))
        waiter.start()
        assert not finished.wait(0.1), "flush returned while its batch was still in flight"
        release.set()
        assert finished.wait(3)
    finally:
        release.set()
        if waiter:
            waiter.join(3)
        client.close()


def test_audit_rejected_items_raise_with_success_http_status(mock_server):
    mock_server.set_responder(lambda _: (200, b'{"accepted":0,"rejected":1}'))
    client = Client(api_key="f0_live_fixture", base_url=mock_server.url, raise_on_error=True)
    try:
        with pytest.raises(TransportError, match="rejected"):
            client.audit.log_batch([event()])
    finally:
        client.close()


def test_telemetry_rejected_items_are_reported(mock_server):
    mock_server.set_responder(lambda _: (200, b'{"accepted_count":0,"errors":["bad parent"]}'))
    client = Client(api_key="f0_live_fixture", base_url=mock_server.url)
    try:
        with pytest.raises(TransportError, match="rejected"):
            client.telemetry._ingest_spans_sync("exec_fixture", [{"id": "span_fixture"}])
    finally:
        client.close()


@pytest.mark.parametrize('asynchronous', [False, True])
def test_execution_context_freezes_occurrence_times_before_delivery(mock_server, monkeypatch, asynchronous):
    import asyncio
    from datetime import datetime, timezone
    from fact0 import AsyncClient
    from fact0.telemetry import context

    start = datetime(2026, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    end = datetime(2026, 1, 1, 0, 0, 21, tzinfo=timezone.utc)
    clock = iter([start, end])
    monkeypatch.setattr(context, '_utcnow', lambda: next(clock))
    mock_server.set_responder(lambda _: (200, b'{"id":"exec_time"}'))

    async def run_async():
        async with AsyncClient(api_key='f0_live_fixture', base_url=mock_server.url, sync=True) as client:
            async with client.telemetry.execution(agent_id='captured-time'):
                pass

    if asynchronous:
        asyncio.run(run_async())
    else:
        client = Client(api_key='f0_live_fixture', base_url=mock_server.url, sync=True)
        try:
            with client.telemetry.execution(agent_id='captured-time'):
                pass
        finally:
            client.close()
    start_request = next(r for r in mock_server.received if r['path'] == '/api/v1/executions')
    end_request = next(r for r in mock_server.received if r['path'].endswith('/end'))
    assert start_request['json']['started_at'] == start.isoformat()
    assert end_request['json']['ended_at'] == end.isoformat()
