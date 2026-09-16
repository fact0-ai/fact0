package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fact0-ai/fact0/internal/ingestion"
)

func TestRequestBodyLimitBoundary(t *testing.T) {
	for _, size := range []int{int(MaxRequestBodyBytes) - 1, int(MaxRequestBodyBytes), int(MaxRequestBodyBytes) + 1} {
		called := false
		handler := RequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			b, _ := io.ReadAll(r.Body)
			if len(b) != size {
				t.Fatalf("body modified: %d", len(b))
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(strings.Repeat(" ", size))))
		if size > int(MaxRequestBodyBytes) {
			if rr.Code != 413 || called {
				t.Fatalf("oversize reached handler: %d", rr.Code)
			}
		} else if rr.Code != 204 || !called {
			t.Fatalf("valid boundary rejected: %d", rr.Code)
		}
	}
}
func TestRequestJSONRetainsLargeInteger(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"n":9007199254740993}`))
	var data map[string]any
	if err := requestJSONDecoder(req).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if n, ok := data["n"].(json.Number); !ok || string(n) != "9007199254740993" {
		t.Fatalf("number lost precision: %#v", data["n"])
	}
}

func TestRequestJSONRejectsTrailingValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true}{"ignored":true}`))
	var data map[string]any
	if err := requestJSONDecoder(r).Decode(&data); err == nil {
		t.Fatal("trailing JSON was silently ignored")
	}
}

func TestExecutionCaptureTimeJSON(t *testing.T) {
	const captured = "2026-09-16T07:51:27.123456789Z"
	want, err := time.Parse(time.RFC3339Nano, captured)
	if err != nil {
		t.Fatal(err)
	}
	var start ingestion.StartExecutionRequest
	r := httptest.NewRequest(http.MethodPost, "/api/v1/executions", strings.NewReader(`{"agent_id":"collector","started_at":"`+captured+`"}`))
	if err := requestJSONDecoder(r).Decode(&start); err != nil || start.StartedAt == nil || !start.StartedAt.Equal(want) {
		t.Fatalf("start capture decode: %+v %v", start, err)
	}
	var end endExecutionRequest
	r = httptest.NewRequest(http.MethodPut, "/api/v1/executions/one/end", strings.NewReader(`{"status":"COMPLETED","ended_at":"`+captured+`"}`))
	if err := requestJSONDecoder(r).Decode(&end); err != nil || end.EndedAt == nil || !end.EndedAt.Equal(want) {
		t.Fatalf("end capture decode: %+v %v", end, err)
	}
	r = httptest.NewRequest(http.MethodPut, "/api/v1/executions/one/end", strings.NewReader(`{"status":"COMPLETED","ended_at":"not-a-date"}`))
	if err := requestJSONDecoder(r).Decode(&end); err == nil {
		t.Fatal("invalid capture time was accepted")
	}
}
