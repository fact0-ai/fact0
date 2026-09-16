package rest

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
)

// fakeHandler always returns 200 OK so the test can tell the difference
// between "allowed through" and "rate-limited".
func fakeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimiter_EventsAllowsBurstThenLimits(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		EventsPerSec:     1,
		EventsBurst:      5,
		ExecutionsPerMin: 60,
	}, zerolog.Nop())
	defer rl.Stop()

	mw := rl.Events()
	h := mw(fakeHandler())

	allowed, limited := 0, 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("POST", "/api/v1/executions/x/spans", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		switch rr.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", rr.Code)
		}
	}

	if allowed > 5 {
		t.Errorf("expected at most 5 allowed (burst), got %d", allowed)
	}
	if limited < 4 {
		t.Errorf("expected at least 4 limited (10 - burst - 1 slack), got %d", limited)
	}
}

func TestRateLimiter_ReturnsRetryAfterHeader(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		EventsPerSec:     1,
		EventsBurst:      1,
		ExecutionsPerMin: 60,
	}, zerolog.Nop())
	defer rl.Stop()

	h := rl.Events()(fakeHandler())

	req1 := httptest.NewRequest("POST", "/api/v1/executions/x/spans", nil)
	req1.RemoteAddr = "10.0.0.2:1234"
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request must pass, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/v1/executions/x/spans", nil)
	req2.RemoteAddr = "10.0.0.2:1234"
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request must be limited, got %d", rr2.Code)
	}
	ra := rr2.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("missing Retry-After header")
	}
	if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Fatalf("Retry-After must be a positive int, got %q", ra)
	}
}

func TestRateLimiter_DifferentIPsHaveSeparateBuckets(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		EventsPerSec:     1,
		EventsBurst:      1,
		ExecutionsPerMin: 60,
	}, zerolog.Nop())
	defer rl.Stop()
	h := rl.Events()(fakeHandler())

	for i, ip := range []string{"10.0.0.10:1", "10.0.0.11:1", "10.0.0.12:1"} {
		req := httptest.NewRequest("POST", "/api/v1/executions/x/spans", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("iter %d ip=%s expected 200, got %d", i, ip, rr.Code)
		}
	}
}
