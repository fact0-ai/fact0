package fact0

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryAfterHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer server.Close()
	client := NewClient(Config{BaseURL: server.URL, MaxRetries: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.doJSON(ctx, "GET", "/", nil, nil, false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("Retry-After ignored context")
	}
}

func TestRetryBackoffHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	client := NewClient(Config{BaseURL: server.URL, MaxRetries: 2})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := client.doJSON(ctx, "GET", "/", nil, nil, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("backoff ignored context")
	}
}
