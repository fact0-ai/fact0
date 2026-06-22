package fact0_test

import (
	"context"
	"testing"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

func TestTelemetryStartExecution(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	_, err := c.Telemetry.StartExecution(context.Background(), fact0.StartExecutionRequest{
		AgentID:   "agent_1",
		AgentName: "demo-agent",
		Trigger:   "manual",
	})
	if err != nil {
		t.Fatalf("start execution: %v", err)
	}

	reqs := ms.received()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != "POST" || req.Path != "/api/v1/executions" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.Path)
	}
	if req.Body["agent_id"] != "agent_1" {
		t.Fatalf("unexpected body: %#v", req.Body)
	}
}

func TestTelemetryEndExecution(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	_, err := c.Telemetry.EndExecution(context.Background(), "exec_123", "success")
	if err != nil {
		t.Fatalf("end execution: %v", err)
	}

	req := ms.received()[0]
	if req.Method != "PUT" || req.Path != "/api/v1/executions/exec_123/end" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.Path)
	}
	if req.Body["status"] != "success" {
		t.Fatalf("unexpected status: %#v", req.Body["status"])
	}
}

func TestTelemetryGetExecution(t *testing.T) {
	ms := newMockServer(t)
	ms.respond = func(rec *recordedRequest) (int, []byte) {
		if rec.Path == "/api/v1/executions/exec_456" {
			return 200, []byte(`{"id":"exec_456","status":"running"}`)
		}
		return 404, []byte(`{"error":"not found"}`)
	}
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	out, err := c.Telemetry.GetExecution(context.Background(), "exec_456")
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if out["id"] != "exec_456" {
		t.Fatalf("unexpected execution: %#v", out)
	}
}

func TestTelemetryIngestSpans(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL: ms.URL,
		APIKey:  "alk_live_test",
	})

	spans := []map[string]any{
		{
			"id":        "span_abc",
			"span_type": "MODEL_INVOCATION",
			"name":      "test-span",
		},
	}

	_, err := c.Telemetry.IngestSpans(context.Background(), "exec_123", spans)
	if err != nil {
		t.Fatalf("ingest spans: %v", err)
	}

	reqs := ms.received()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != "POST" || req.Path != "/api/v1/executions/exec_123/spans" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.Path)
	}
}

func TestTelemetryIngestEvents(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL: ms.URL,
		APIKey:  "alk_live_test",
	})

	events := []map[string]any{
		{
			"id":         "event_abc",
			"event_type": "log",
			"payload":    map[string]any{"msg": "test"},
		},
	}

	_, err := c.Telemetry.IngestEvents(context.Background(), "exec_123", events)
	if err != nil {
		t.Fatalf("ingest events: %v", err)
	}

	reqs := ms.received()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != "POST" || req.Path != "/api/v1/executions/exec_123/events" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.Path)
	}
}
