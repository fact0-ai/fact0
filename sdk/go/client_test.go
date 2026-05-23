package fact0_test

import (
	"context"
	"testing"
	"time"

	fact0 "github.com/fact0-ai/fact0-go"
)

func TestNewClientDefaults(t *testing.T) {
	c := fact0.NewClient(fact0.Config{APIKey: "alk_live_test"})
	if c.Audit == nil || c.Telemetry == nil {
		t.Fatal("expected audit and telemetry clients")
	}
}

func TestClientSyncIngestHeader(t *testing.T) {
	ms := newMockServer(t)
	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		SyncIngest: true,
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
	if reqs[0].Sync != "true" {
		t.Fatalf("expected X-Fact0-Sync header, got %q", reqs[0].Sync)
	}
}

func TestClientRetriesOn503(t *testing.T) {
	ms := newMockServer(t)
	calls := 0
	ms.respond = func(*recordedRequest) (int, []byte) {
		calls++
		if calls == 1 {
			return 503, []byte(`{"error":"unavailable"}`)
		}
		return 200, []byte(`{"ok":true}`)
	}

	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 2,
		Timeout:    5 * time.Second,
	})

	err := c.Audit.Log(context.Background(), sampleAuditEvent())
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected retry, got %d calls", calls)
	}
}

func TestClientNonRetryableError(t *testing.T) {
	ms := newMockServer(t)
	ms.respond = func(*recordedRequest) (int, []byte) {
		return 400, []byte(`{"error":"bad request"}`)
	}

	c := fact0.NewClient(fact0.Config{
		BaseURL:    ms.URL,
		APIKey:     "alk_live_test",
		MaxRetries: 2,
		Timeout:    5 * time.Second,
	})

	err := c.Audit.Log(context.Background(), sampleAuditEvent())
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func sampleAuditEvent() fact0.AuditEventInput {
	return fact0.AuditEventInput{
		Actor:    fact0.Actor{ID: "user_1", Type: "human"},
		Action:   "document.read",
		Resource: fact0.Resource{ID: "doc_1", Type: "document"},
		Outcome:  "success",
	}
}
