package ingestion

import (
	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/redaction"
)

// redactionMarkerKey is the metadata key set on spans/events whose
// contents were modified by the redactor. The UI surfaces this as a
// "Redacted" badge on the execution detail view.
const redactionMarkerKey = "redacted"

// redactExecution scrubs the top-level metadata map. Execution rows
// have no payload columns of their own; everything sensitive lives in
// metadata.
func (s *Service) redactExecution(e *execution.Execution) {
	if redaction.RedactStringMap(e.Metadata) {
		e.Metadata[redactionMarkerKey] = "true"
	}
}

// redactSpan walks every place a span carries user-supplied strings:
// generic metadata, the typed detail blocks (tool_call, model_invocation,
// state_mutation, human_approval, policy_evaluation) and the error
// message + stack trace.
func (s *Service) redactSpan(sp *execution.Span) {
	touched := redaction.RedactStringMap(sp.Metadata)

	if sp.ToolCall != nil {
		if redactPayload(sp.ToolCall.Input) {
			touched = true
		}
		if redactPayload(sp.ToolCall.Output) {
			touched = true
		}
	}
	if sp.ModelInvocation != nil {
		if redactPayload(sp.ModelInvocation.Prompt) {
			touched = true
		}
		if redactPayload(sp.ModelInvocation.Completion) {
			touched = true
		}
	}
	if sp.StateMutation != nil {
		if redactPayload(sp.StateMutation.BeforeValue) {
			touched = true
		}
		if redactPayload(sp.StateMutation.AfterValue) {
			touched = true
		}
	}
	if sp.HumanApproval != nil {
		if redacted, c := redaction.Redact(sp.HumanApproval.Reasoning); c {
			sp.HumanApproval.Reasoning = redacted
			touched = true
		}
	}
	if sp.PolicyEvaluation != nil {
		if redaction.RedactStringMap(sp.PolicyEvaluation.Context) {
			touched = true
		}
	}
	if sp.Error != nil {
		if r, c := redaction.Redact(sp.Error.Message); c {
			sp.Error.Message = r
			touched = true
		}
		if r, c := redaction.Redact(sp.Error.StackTrace); c {
			sp.Error.StackTrace = r
			touched = true
		}
	}

	if touched {
		if sp.Metadata == nil {
			sp.Metadata = map[string]string{}
		}
		sp.Metadata[redactionMarkerKey] = "true"
	}
}

func (s *Service) redactEvent(e *execution.ExecutionEvent) {
	touched := redaction.RedactStringMap(e.Metadata)
	if redactPayload(e.Payload) {
		touched = true
	}
	if touched {
		if e.Metadata == nil {
			e.Metadata = map[string]string{}
		}
		e.Metadata[redactionMarkerKey] = "true"
	}
}

// redactPayload scrubs a PayloadRef's Inline value in place. Other
// fields (SizeBytes, ContentType) are descriptive metadata and don't
// need scrubbing.
func redactPayload(p *execution.PayloadRef) bool {
	if p == nil {
		return false
	}
	nv, c := redaction.RedactInline(p.Inline)
	if c {
		p.Inline = nv
	}
	return c
}
