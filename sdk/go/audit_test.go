package fact0_test

import (
	"context"
	"testing"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

func TestAuditLog(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	err := c.Audit.Log(context.Background(), sampleAuditEvent())
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	reqs := ms.received()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != "POST" || req.Path != "/v1/events/batch" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.Path)
	}
	if req.Auth != "Bearer alk_live_test" {
		t.Fatalf("unexpected auth: %q", req.Auth)
	}
	events, ok := req.Body["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("expected one event in batch, got %#v", req.Body["events"])
	}
}

func TestAuditLogBatch(t *testing.T) {
	ms := newMockServer(t)
	ms.respond = func(*recordedRequest) (int, []byte) {
		return 200, []byte(`{"accepted":2,"rejected":0}`)
	}
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	out, err := c.Audit.LogBatch(context.Background(), []fact0.AuditEventInput{
		sampleAuditEvent(),
		sampleAuditEvent(),
	})
	if err != nil {
		t.Fatalf("log batch: %v", err)
	}
	if out["accepted"] != float64(2) {
		t.Fatalf("unexpected response: %#v", out)
	}
}

func TestAuditGetEvent(t *testing.T) {
	ms := newMockServer(t)
	ms.respond = func(rec *recordedRequest) (int, []byte) {
		if rec.Method == "GET" && rec.Path == "/v1/events/evt_1" {
			return 200, []byte(`{"id":"evt_1"}`)
		}
		return 404, []byte(`{"error":"not found"}`)
	}
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	})

	out, err := c.Audit.GetEvent(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if out["id"] != "evt_1" {
		t.Fatalf("unexpected event: %#v", out)
	}
}
