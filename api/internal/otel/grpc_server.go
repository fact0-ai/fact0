package otel

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/ingestion"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// GRPCServer implements the OTLP TraceService gRPC service.
// It runs on a separate port (default :4317, the IANA-registered OTLP port)
// and shares the same Translator and ingestion.Service as the HTTP handler.
type GRPCServer struct {
	collectorpb.UnimplementedTraceServiceServer
	ingestion  *ingestion.Service
	translator *Translator
	keyStore   audit.KeyStore
	logger     zerolog.Logger
	server     *grpc.Server
}

// NewGRPCServer creates an OTLP/gRPC server.
func NewGRPCServer(
	ingestionSvc *ingestion.Service,
	keyStore audit.KeyStore,
	logger zerolog.Logger,
) *GRPCServer {
	s := &GRPCServer{
		ingestion:  ingestionSvc,
		translator: NewTranslator(),
		keyStore:   keyStore,
		logger:     logger.With().Str("component", "otlp-grpc").Logger(),
	}

	s.server = grpc.NewServer(
		grpc.UnaryInterceptor(s.authInterceptor()),
	)
	collectorpb.RegisterTraceServiceServer(s.server, s)

	return s
}

// Export implements TraceService.Export - the core OTLP gRPC endpoint.
// OTel SDKs and collectors call this method to send trace data.
func (s *GRPCServer) Export(
	ctx context.Context,
	req *collectorpb.ExportTraceServiceRequest,
) (*collectorpb.ExportTraceServiceResponse, error) {
	tenantID := auth.TenantFromContext(ctx)
	if tenantID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	traces := s.translator.TranslateResourceSpans(tenantID, req.ResourceSpans)
	if len(traces) == 0 {
		return &collectorpb.ExportTraceServiceResponse{}, nil
	}

	totalSpans := 0
	totalExecs := 0
	for _, tt := range traces {
		_, err := s.ingestion.StartExecution(ctx, tenantID, ingestion.StartExecutionRequest{
			AgentID:        tt.Execution.AgentID,
			AgentName:      tt.Execution.AgentName,
			Trigger:        tt.Execution.Trigger,
			Metadata:       tt.Execution.Metadata,
			IdempotencyKey: tt.Execution.ID,
		})
		if err != nil {
			s.logger.Warn().Err(err).
				Str("execution_id", tt.Execution.ID).
				Msg("failed to start execution from OTLP/gRPC trace")
			metrics.IngestRejected.WithLabelValues("otlp_grpc_execution", "storage").Inc()
			continue
		}
		totalExecs++

		if len(tt.Spans) > 0 {
			accepted, errs := s.ingestion.IngestSpans(ctx, tenantID, tt.Execution.ID, tt.Spans)
			totalSpans += accepted
			if len(errs) > 0 {
				s.logger.Warn().
					Str("execution_id", tt.Execution.ID).
					Int("accepted", accepted).
					Int("errors", len(errs)).
					Msg("partial OTLP/gRPC span ingestion")
			}
		}

		if tt.Execution.Status == "COMPLETED" || tt.Execution.Status == "FAILED" {
			_, _ = s.ingestion.EndExecution(ctx, tenantID, tt.Execution.ID, tt.Execution.Status)
		}
	}

	metrics.IngestAccepted.WithLabelValues("otlp_grpc_trace").Add(float64(totalExecs))
	metrics.IngestAccepted.WithLabelValues("otlp_grpc_span").Add(float64(totalSpans))

	s.logger.Info().
		Str("tenant_id", tenantID).
		Int("traces", len(traces)).
		Int("executions", totalExecs).
		Int("spans", totalSpans).
		Msg("OTLP/gRPC traces ingested")

	return &collectorpb.ExportTraceServiceResponse{}, nil
}

// ListenAndServe starts the gRPC server on the given port.
func (s *GRPCServer) ListenAndServe(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on :%d: %w", port, err)
	}
	return s.server.Serve(lis)
}

// GracefulStop stops the gRPC server, waiting for in-flight RPCs to complete.
func (s *GRPCServer) GracefulStop() {
	s.server.GracefulStop()
}

// authInterceptor returns a gRPC unary interceptor that extracts the API key
// from the "authorization" gRPC metadata field. This mirrors the HTTP
// auth.APIKey middleware so both paths resolve keys identically.
//
// OTel SDKs send headers as gRPC metadata when configured via:
//
//	OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer f0_live_xxxx"
func (s *GRPCServer) authInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Extract bearer token from "authorization" metadata key.
		// gRPC metadata keys are lowercase.
		authVals := md.Get("authorization")
		if len(authVals) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		raw := strings.TrimSpace(strings.TrimPrefix(authVals[0], "Bearer "))
		if !auth.LooksLikeKey(raw) {
			return nil, status.Error(codes.Unauthenticated, "malformed api key")
		}

		id := ""
		stripped := strings.TrimPrefix(raw, auth.KeyPrefixLive)
		parts := strings.SplitN(stripped, ".", 2)
		if len(parts) == 2 {
			id = parts[0]
		} else {
			return nil, status.Error(codes.Unauthenticated, "invalid api key format")
		}

		k, err := s.keyStore.GetKeyByID(ctx, id)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid api key")
		}
		if _, ok := auth.VerifyAPIKey(raw, k.Hash); !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid api key")
		}
		if k.RevokedAt != nil {
			return nil, status.Error(codes.Unauthenticated, "api key revoked")
		}
		if !k.Scope.CanWrite() {
			return nil, status.Error(codes.PermissionDenied, "write scope required")
		}

		ctx = auth.WithTenant(ctx, k.TenantID, k.Scope, k.ID)
		return handler(ctx, req)
	}
}
