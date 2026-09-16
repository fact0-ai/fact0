package config

import (
	"context"
	"testing"
)

func TestOSSProfileOverridesHostedFeatures(t *testing.T) {
	t.Setenv("FACT0_OSS_MODE", "true")
	t.Setenv("FACT0_POSTGRES_DSN", "postgres://test@example.invalid/db")
	t.Setenv("FACT0_INGEST_WORKER_ENABLED", "true")
	t.Setenv("FACT0_INGEST_ASYNC_DEFAULT", "true")
	t.Setenv("FACT0_OTLP_HTTP_ENABLED", "true")
	t.Setenv("FACT0_OTLP_GRPC_ENABLED", "true")
	t.Setenv("FACT0_TRANSPARENCY_LOG_ENABLED", "true")
	t.Setenv("FACT0_EMAIL_BRIDGE_URL", "https://example.invalid/mail")
	cfg, err := Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Server
	if !s.OSSMode || !s.RetentionDisabled || !s.DigestDisabled || s.IngestWorkerEnabled || s.IngestAsyncDefault || s.OTLPHTTPEnabled || s.OTLPGRPCEnabled || s.TransparencyEnabled || s.EmailBridgeURL != "" || s.MaxBatchBodyBytes != 4<<20 {
		t.Fatal("OSS hosted feature override failed")
	}
}
