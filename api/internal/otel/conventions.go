package otel

// ─── GenAI Semantic Conventions ─────────────────────────────
// https://opentelemetry.io/docs/specs/semconv/gen-ai/
const (
	SemConvGenAISystem           = "gen_ai.system"
	SemConvGenAIRequestModel     = "gen_ai.request.model"
	SemConvGenAIResponseModel    = "gen_ai.response.model"
	SemConvGenAIPromptTokens     = "gen_ai.usage.prompt_tokens"
	SemConvGenAICompletionTokens = "gen_ai.usage.completion_tokens"
	SemConvGenAITotalTokens      = "gen_ai.usage.total_tokens"
	SemConvGenAITemperature      = "gen_ai.request.temperature"
	SemConvGenAIMaxTokens        = "gen_ai.request.max_tokens"
	SemConvGenAITopP             = "gen_ai.request.top_p"
	SemConvGenAIFinishReason     = "gen_ai.response.finish_reasons"
)

// ─── Database Semantic Conventions ──────────────────────────
// https://opentelemetry.io/docs/specs/semconv/database/
const (
	SemConvDBSystem    = "db.system"
	SemConvDBStatement = "db.statement"
	SemConvDBOperation = "db.operation"
	SemConvDBName      = "db.name"
)

// ─── HTTP Semantic Conventions ──────────────────────────────
// https://opentelemetry.io/docs/specs/semconv/http/
const (
	SemConvHTTPMethod     = "http.request.method"
	SemConvHTTPURL        = "url.full"
	SemConvHTTPStatusCode = "http.response.status_code"
)

// ─── Exception Semantic Conventions ─────────────────────────
// https://opentelemetry.io/docs/specs/semconv/exceptions/
const (
	SemConvExceptionType       = "exception.type"
	SemConvExceptionMessage    = "exception.message"
	SemConvExceptionStacktrace = "exception.stacktrace"
)
