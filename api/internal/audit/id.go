package audit

import (
	cryptorand "crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ULIDs are time-sortable, URL-safe and deterministic in length, matching
// the brief's "evt_01HX3K..." example. We share a single monotonic entropy
// source guarded by a mutex to ensure strictly increasing IDs within a ms.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(cryptorand.Reader, 0)
)

// NewEventID returns a new event identifier of the form `evt_<ULID>`.
func NewEventID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "evt_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewKeyID returns a new opaque API key row identifier.
func NewKeyID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "akid_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewTenantID returns a fresh internal tenant identifier. We mint these
// locally rather than reusing the auth provider's org id so swapping
// providers is a config change, not a primary-key migration across audit_events.
func NewTenantID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "tnt_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewReceiptID returns an async ingest receipt identifier.
func NewReceiptID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "rcpt_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewShareLinkID returns a read-only share link identifier.
func NewShareLinkID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "shr_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}
