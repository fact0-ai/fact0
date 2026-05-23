# fact0

Python SDK for [Fact0](https://fact0.io) - tamper-evident audit logs and execution telemetry for AI agents.

```bash
pip install fact0-sdk
```

```python
import fact0

client = fact0.Client(api_key="alk_live_...")

client.audit.log(
    actor={"id": "user_123", "type": "human"},
    action="document.delete",
    resource={"id": "doc_456", "type": "document"},
    outcome="success",
)

with client.telemetry.execution(agent_id="bot-1") as ex:
    with ex.span("tool.search", span_type="TOOL_CALL") as span:
        span.complete(output={"hits": 3})
```

Docs: [docs.fact0.io](https://docs.fact0.io)
