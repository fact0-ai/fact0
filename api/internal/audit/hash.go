package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/fact0-ai/fact0/internal/canonical"
	"time"
)

// HashPrefix versions the canonical recipe. V1 hashes are not imported by
// the fresh-only OSS schema installer.
const HashPrefix = "sha256:v2:"

// ComputeHash binds every immutable public event field except the digest
// itself. JSON is unambiguous for embedded newlines and sorted maps; numeric
// normalization matches JSONB and timestamps match TIMESTAMPTZ precision.
// SequenceNumber must be allocated before hashing for persistence.
func ComputeHash(e *AuditEvent) string {
	if e == nil {
		return ""
	}
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	b, err := canonical.JSON(struct {
		ID             string         `json:"id"`
		TenantID       string         `json:"tenant_id"`
		Timestamp      string         `json:"timestamp"`
		Actor          Actor          `json:"actor"`
		Action         string         `json:"action"`
		Resource       Resource       `json:"resource"`
		Outcome        Outcome        `json:"outcome"`
		Metadata       map[string]any `json:"metadata"`
		PrevHash       string         `json:"prev_hash"`
		SequenceNumber int64          `json:"sequence_number"`
	}{e.ID, e.TenantID, e.Timestamp.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"), e.Actor, e.Action, e.Resource, e.Outcome, metadata, e.PrevHash, e.SequenceNumber})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return HashPrefix + hex.EncodeToString(sum[:])
}
