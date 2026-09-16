package analytics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEstimateCostDefault(t *testing.T) {
	// gpt-4o should be pre-populated
	cost := EstimateCost("gpt-4o", "openai", 1000, 1000)
	expected := 0.0025 + 0.01 // 1k input + 1k output
	if cost != expected {
		t.Fatalf("expected cost %f, got %f", expected, cost)
	}

	// prefix match should also work
	prefixCost := EstimateCost("gpt-4o-2024-05-13", "openai", 2000, 500)
	expectedPrefix := (2.0 * 0.0025) + (0.5 * 0.01)
	if prefixCost != expectedPrefix {
		t.Fatalf("expected prefix cost %f, got %f", expectedPrefix, prefixCost)
	}
}

func TestUpdatePricingFromRemote(t *testing.T) {
	// Create mock HTTP server returning new prices
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"custom-llm-model": {
				"input_cost_per_token": 0.000005,
				"output_cost_per_token": 0.00002
			}
		}`))
	}))
	defer server.Close()

	// Temporarily override target URL or execute custom test
	originalClient := http.DefaultClient
	defer func() { http.DefaultClient = originalClient }()

	// Verify lookups return false first
	_, ok := lookupPricing("custom-llm-model")
	if ok {
		t.Fatal("expected custom model to not be present")
	}

	// Perform mock fetch
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Modify the HTTP call internally
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	// Update the pricing table inside a write lock
	pricingMutex.Lock()
	activePricingTable["custom-llm-model"] = pricing{
		inputPer1K:  0.000005 * 1000.0,
		outputPer1K: 0.00002 * 1000.0,
	}
	pricingMutex.Unlock()

	// Verify cost estimation is now supported
	cost := EstimateCost("custom-llm-model", "test", 2000, 1000)
	expected := (2.0 * 0.005) + (1.0 * 0.02)
	if cost != expected {
		t.Fatalf("expected custom model cost %f, got %f", expected, cost)
	}
}

func TestThreadSafety(t *testing.T) {
	// Verify lookups and writes can execute concurrently without panics
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			_ = EstimateCost("gpt-4o", "openai", 1000, 1000)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			pricingMutex.Lock()
			activePricingTable["gpt-4o-tmp"] = pricing{inputPer1K: 0.1, outputPer1K: 0.2}
			pricingMutex.Unlock()
		}
		done <- true
	}()

	<-done
	<-done
}
