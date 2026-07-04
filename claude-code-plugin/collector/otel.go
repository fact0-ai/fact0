package main

import "encoding/json"

// MaybeModelInvocationSpan builds a MODEL_INVOCATION span when the hook input
// carries model cost/usage telemetry. It returns (span, true) IFF
// in.TotalCostUSD > 0 OR in.Usage is non-empty and parseable; otherwise
// (nil, false). It is defensive and never panics on malformed usage.
func MaybeModelInvocationSpan(in HookInput, executionID, parentSpanID string) (map[string]any, bool) {
	// Attempt to parse usage; tolerate malformed/empty payloads.
	usage := map[string]any{}
	hasUsage := false
	if len(in.Usage) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(in.Usage, &parsed); err == nil && len(parsed) > 0 {
			usage = parsed
			hasUsage = true
		}
	}

	if in.TotalCostUSD <= 0 && !hasUsage {
		return nil, false
	}

	mi := map[string]any{
		"model_name": "claude",
		"cost_usd":   in.TotalCostUSD,
	}

	// Best-effort model name from usage, if present.
	for _, k := range []string{"model", "model_name"} {
		if v, ok := usage[k].(string); ok && v != "" {
			mi["model_name"] = v
			break
		}
	}

	// Best-effort token counts; numbers decode as float64 from JSON.
	copyToken := func(srcKeys []string, dst string) {
		for _, k := range srcKeys {
			if v, ok := usageNumber(usage[k]); ok {
				mi[dst] = v
				return
			}
		}
	}
	// Destination keys must match the server's typed ModelInvocationDetail
	// json tags (prompt_tokens/completion_tokens/total_tokens) — the ingest
	// decode silently drops unknown keys.
	copyToken([]string{"input_tokens", "prompt_tokens"}, "prompt_tokens")
	copyToken([]string{"output_tokens", "completion_tokens"}, "completion_tokens")
	copyToken([]string{"total_tokens", "total"}, "total_tokens")

	now := nowRFC3339()
	span := map[string]any{
		"id":               newSpanID(),
		"execution_id":     executionID,
		"span_type":        "MODEL_INVOCATION",
		"name":             "model_invocation",
		"status":           "COMPLETED",
		"started_at":       now,
		"ended_at":         now,
		"model_invocation": mi,
		"metadata":         map[string]any{},
	}
	if parentSpanID != "" {
		span["parent_span_id"] = parentSpanID
	}
	return span, true
}

// usageNumber coerces a JSON-decoded value to a float64 token count.
func usageNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
