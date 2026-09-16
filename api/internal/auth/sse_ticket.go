package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
)

// SSETicket is a single-use, short-lived credential the dashboard
// exchanges its JWT for before opening an EventSource. We use tickets
// (rather than putting the JWT or API key in the URL) because
// EventSource cannot set Authorization headers and we don't want
// credential material leaking into browser history, access logs, or
// Referer chains.
type SSETicket struct {
	TenantID  string
	Scope     audit.Scope
	ExpiresAt time.Time
}

// SSETicketStore issues and validates HMAC-SHA256-signed tickets.
//
// Unlike the previous in-memory implementation, tickets are self-contained
// signed tokens - any backend instance can verify them without shared state.
// This is critical when running multiple ECS tasks behind a load balancer:
// POST /v1/me/sse-ticket and GET /v1/events/stream may land on different
// instances, and in-memory maps don't survive that round-trip.
//
// Trade-off: we lose single-use enforcement (a valid token can be replayed
// within its TTL window). The short TTL (default 30s) and HTTPS mitigate
// practical replay risk for a dashboard SSE stream.
type SSETicketStore struct {
	key []byte
}

type ticketPayload struct {
	TenantID string `json:"t"`
	Scope    string `json:"s"`
	Exp      int64  `json:"e"` // Unix seconds
}

// NewSSETicketStore constructs the store. If secret is non-empty its
// SHA-256 hash is used as the HMAC key (recommended in production;
// reuse FACT0_AUTH_WEBHOOK_SECRET or any stable shared secret).
// If secret is empty a random 32-byte key is generated at startup -
// fine for single-instance dev but incompatible with multi-task deploys.
func NewSSETicketStore(secret string) *SSETicketStore {
	var key []byte
	if secret != "" {
		h := sha256.Sum256([]byte(secret))
		key = h[:]
	} else {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic("sse_ticket: cannot generate key: " + err.Error())
		}
	}
	return &SSETicketStore{key: key}
}

// Issue mints a signed ticket valid for ttl.
// Format: tkt_<base64url(payload)>.<base64url(HMAC-SHA256)>
func (s *SSETicketStore) Issue(tenantID string, scope audit.Scope, ttl time.Duration) (string, error) {
	p := ticketPayload{
		TenantID: tenantID,
		Scope:    string(scope),
		Exp:      time.Now().Add(ttl).Unix(),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	sig := s.sign(b64)
	return "tkt_" + b64 + "." + sig, nil
}

// Consume validates the ticket and returns it if unexpired and
// correctly signed. There is no delete step - the ticket remains
// valid until its TTL expires, but the short window (≤30s in
// production) limits the replay surface.
func (s *SSETicketStore) Consume(tok string) (SSETicket, bool) {
	body := strings.TrimPrefix(tok, "tkt_")
	dot := strings.LastIndex(body, ".")
	if dot < 0 {
		return SSETicket{}, false
	}
	b64, sig := body[:dot], body[dot+1:]

	// Constant-time MAC comparison.
	if !hmac.Equal([]byte(sig), []byte(s.sign(b64))) {
		return SSETicket{}, false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return SSETicket{}, false
	}
	var p ticketPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return SSETicket{}, false
	}
	if time.Now().Unix() > p.Exp {
		return SSETicket{}, false
	}
	return SSETicket{
		TenantID:  p.TenantID,
		Scope:     audit.Scope(p.Scope),
		ExpiresAt: time.Unix(p.Exp, 0),
	}, true
}

func (s *SSETicketStore) sign(data string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
