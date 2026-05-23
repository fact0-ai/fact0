package fact0_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type recordedRequest struct {
	Method string
	Path   string
	Auth   string
	Sync   string
	Body   map[string]any
}

type mockServer struct {
	URL      string
	mu       sync.Mutex
	Requests []recordedRequest
	respond  func(*recordedRequest) (int, []byte)
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()
	ms := &mockServer{
		respond: func(*recordedRequest) (int, []byte) {
			return http.StatusOK, []byte(`{"ok":true}`)
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if len(body) > 0 {
			_ = json.Unmarshal(body, &payload)
		}
		rec := recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Sync:   r.Header.Get("X-Fact0-Sync"),
			Body:   payload,
		}
		ms.mu.Lock()
		ms.Requests = append(ms.Requests, rec)
		ms.mu.Unlock()

		status, respBody := ms.respond(&rec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(respBody)
	}))
	ms.URL = srv.URL
	t.Cleanup(srv.Close)
	return ms
}

func (ms *mockServer) received() []recordedRequest {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := make([]recordedRequest, len(ms.Requests))
	copy(out, ms.Requests)
	return out
}
