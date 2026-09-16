package rest

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"github.com/fact0-ai/fact0/internal/audit"
)

type routeKey struct {
	method string
	path   string
}

func openapiPaths(t *testing.T, rel string) map[routeKey]struct{} {
	t.Helper()
	specPath := filepath.Join(repoRoot(t), rel)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	out := make(map[routeKey]struct{})
	for p, methods := range doc.Paths {
		for m := range methods {
			switch strings.ToLower(m) {
			case "get", "post", "put", "patch", "delete", "head", "options":
				out[routeKey{method: strings.ToUpper(m), path: p}] = struct{}{}
			}
		}
	}
	return out
}

func chiRoutes(r chi.Router) map[routeKey]struct{} {
	out := make(map[routeKey]struct{})
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[routeKey{method: method, path: route}] = struct{}{}
		return nil
	})
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func routesRegistered(registered map[routeKey]struct{}, spec map[routeKey]struct{}) []routeKey {
	var missing []routeKey
	for rk := range spec {
		found := false
		for reg := range registered {
			if reg.method == rk.method && reg.path == rk.path {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, rk)
		}
	}
	return missing
}

func TestOpenAPIAuditRoutesMatchHandlers(t *testing.T) {
	repo := newFakeRepo()
	svc := audit.NewService(repo, zerolog.Nop(), false)
	h := NewAuditHandler(svc, nil, nil, zerolog.Nop(), AuditHandlerConfig{OSSMode: true})

	r := chi.NewRouter()
	h.Mount(r)

	for _, rk := range routesRegistered(chiRoutes(r), openapiPaths(t, "openapi/audit.v1.yaml")) {
		t.Errorf("OpenAPI declares %s %s but no chi route registered", rk.method, rk.path)
	}
}

func TestOpenAPITelemetryRoutesMatchHandlers(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	h.MountTelemetry(r, nil, nil)

	for _, rk := range routesRegistered(chiRoutes(r), openapiPaths(t, "openapi/telemetry.v1.yaml")) {
		t.Errorf("OpenAPI declares %s %s but no chi route registered", rk.method, rk.path)
	}
}
