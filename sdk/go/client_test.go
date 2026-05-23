package fact0_test

import (
	"context"
	"os"
	"testing"

	fact0 "github.com/fact0-ai/fact0-go"
)

func TestNewClient(t *testing.T) {
	c := fact0.NewClient(fact0.Config{
		BaseURL: "http://localhost:8000",
		APIKey:  os.Getenv("FACT0_API_KEY"),
	})
	if c.Audit == nil || c.Telemetry == nil {
		t.Fatal("expected audit and telemetry clients")
	}
	_ = context.Background()
}
