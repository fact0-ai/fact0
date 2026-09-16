package execution

import (
	"crypto/sha256"
	"fmt"
)

// GenerateExecutionID produces a deterministic execution ID from its inputs.
func GenerateExecutionID(agentID string, startTime int64, idempotencyKey string) string {
	input := fmt.Sprintf("exec:%s:%d:%s", agentID, startTime, idempotencyKey)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("exec_%x", hash[:8])
}

// GenerateSpanID produces a deterministic span ID.
func GenerateSpanID(executionID string, parentSpanID string, name string, seq int64) string {
	input := fmt.Sprintf("span:%s:%s:%s:%d", executionID, parentSpanID, name, seq)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("span_%x", hash[:8])
}

// GenerateEventID produces a deterministic event ID.
func GenerateEventID(executionID string, spanID string, eventType string, seq int64) string {
	input := fmt.Sprintf("evt:%s:%s:%s:%d", executionID, spanID, eventType, seq)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("evt_%x", hash[:8])
}
