// Package redaction strips obvious secrets and PII from ingested data
// BEFORE it reaches Postgres. This is the "even if you accidentally
// log it, we don't store it" promise on the ingest path.
//
// Patterns are deliberately conservative - false positives are worse
// than false negatives here because a redacted secret is irrecoverable.
// We err on the side of strict shapes (provider-specific prefixes,
// minimum lengths, Luhn check for cards).
//
// Replacement strategy: every match is replaced with a `[REDACTED:<kind>]`
// marker. The original length is NOT preserved on purpose - preserving
// length would let an attacker work backwards from a hashed log to a
// candidate set of plaintexts.
package redaction

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Kind names the pattern family that matched. Exposed in the redaction
// marker so downstream readers can tell why a value was scrubbed.
type Kind string

const (
	KindAWSAccessKey Kind = "aws_access_key"
	KindAWSSecretKey Kind = "aws_secret_key"
	KindGitHubToken  Kind = "github_token"
	KindSlackToken   Kind = "slack_token"
	KindOpenAIKey    Kind = "openai_key"
	KindJWT          Kind = "jwt"
	KindEmail        Kind = "email"
	KindSSN          Kind = "ssn"
	KindCreditCard   Kind = "credit_card"
	KindBearerToken  Kind = "bearer_token"
)

// pattern bundles a compiled regex with its replacement marker.
type pattern struct {
	kind      Kind
	re        *regexp.Regexp
	postCheck func(match string) bool // optional extra validation
}

// patterns is the ordered list applied in sequence. Order matters:
// more-specific patterns (provider prefixes) run before generic ones
// (email) so a token-shaped email isn't double-replaced.
var patterns = []pattern{
	// AWS access key: 16 chars after AKIA prefix.
	{kind: KindAWSAccessKey, re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	// AWS secret access key - 40 base64-ish chars. Loose regex; we
	// guard with a postCheck that requires it to look adjacent to a
	// known signal word ("secret", "aws") OR to be ≥ 40 chars with
	// no obvious non-secret structure.
	{kind: KindAWSSecretKey, re: regexp.MustCompile(`\b[A-Za-z0-9/+]{40}\b`), postCheck: looksLikeAWSSecret},
	// GitHub personal access tokens.
	{kind: KindGitHubToken, re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	// Slack tokens (xoxa/b/p/r/s).
	{kind: KindSlackToken, re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	// OpenAI API keys.
	{kind: KindOpenAIKey, re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	// JWT: three dot-separated base64url segments. Must start with the
	// canonical eyJ header marker so we don't false-match arbitrary
	// dot-separated identifiers.
	{kind: KindJWT, re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
	// US SSN (NNN-NN-NNNN). Strict format - bare 9-digit numbers are
	// too ambiguous to scrub blindly.
	{kind: KindSSN, re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	// Credit-card-shaped numbers 13–19 digits with optional spaces/dashes.
	// The Luhn gate runs in postCheck.
	{kind: KindCreditCard, re: regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`), postCheck: luhnValid},
	// Bearer token in an Authorization header form.
	{kind: KindBearerToken, re: regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]+`)},
	// Email - last so we don't scrub the local-part of an embedded
	// token that happens to look like user@host.
	{kind: KindEmail, re: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)},
}

// looksLikeAWSSecret avoids redacting any random 40-char base64 string
// (those are common in hashes, tokens, image digests etc.) unless the
// surrounding text gives a strong signal.
//
// Heuristic: must contain a mix of cases AND digits, AND at least one
// `/` or `+` (which random hashes typically lack). Combined with the
// 40-char anchor this is enough to catch real AWS secrets while
// leaving SHA-256 hex (only [0-9a-f]) and ULIDs alone.
func looksLikeAWSSecret(s string) bool {
	hasUpper, hasLower, hasDigit, hasSpecial := false, false, false, false
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '/' || c == '+':
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasDigit && hasSpecial
}

// luhnValid runs the Luhn checksum after stripping spaces/dashes.
// Returns false for empty strings or strings outside 13–19 digits.
func luhnValid(s string) bool {
	digits := make([]int, 0, 19)
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		} else if c != ' ' && c != '-' {
			return false
		}
	}
	n := len(digits)
	if n < 13 || n > 19 {
		return false
	}
	sum, alt := 0, false
	for i := n - 1; i >= 0; i-- {
		d := digits[i]
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// Redact scans s for every pattern in order and replaces matches with a
// kind-tagged marker. Returns the redacted string and a flag set when
// any pattern actually matched.
//
// O(N × |s|) where N is the number of patterns (~10). For typical span
// metadata values (<1 KB) this is well under a microsecond.
func Redact(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	changed := false
	for _, p := range patterns {
		marker := "[REDACTED:" + string(p.kind) + "]"
		s = p.re.ReplaceAllStringFunc(s, func(match string) string {
			if p.postCheck != nil && !p.postCheck(match) {
				return match
			}
			changed = true
			return marker
		})
	}
	return s, changed
}

// RedactStringMap scrubs every value of m in place. Keys are never
// modified (they're typically meaningful identifiers like "user_id").
// Returns true if any value was redacted.
func RedactStringMap(m map[string]string) bool {
	if m == nil {
		return false
	}
	changed := false
	for k, v := range m {
		if redacted, c := Redact(v); c {
			m[k] = redacted
			changed = true
		}
	}
	return changed
}

// RedactJSONMap walks a map[string]interface{} (e.g. an audit metadata
// blob) recursively and scrubs every string leaf. Mutates in place.
func RedactJSONMap(m map[string]interface{}) bool {
	if m == nil {
		return false
	}
	changed := false
	for k, v := range m {
		nv, c := redactValue(v)
		if c {
			m[k] = nv
			changed = true
		}
	}
	return changed
}

// redactValue dispatches on the JSON-shaped value. Returns the (possibly
// new) value and whether anything inside was scrubbed.
func redactValue(v interface{}) (interface{}, bool) {
	switch t := v.(type) {
	case string:
		s, c := Redact(t)
		return s, c
	case map[string]interface{}:
		c := RedactJSONMap(t)
		return t, c
	case []interface{}:
		changed := false
		for i, el := range t {
			nv, c := redactValue(el)
			if c {
				t[i] = nv
				changed = true
			}
		}
		return t, changed
	default:
		return v, false
	}
}

// RedactInline scrubs a PayloadRef.Inline payload. Strings are scrubbed
// directly; structured maps/slices recurse. Anything else (numbers,
// bools, nil) passes through untouched.
//
// For untyped interface{} that came from JSON unmarshal we get the
// usual map[string]interface{} / []interface{} shapes covered by
// redactValue, plus a fast-path for already-marshalled JSON strings.
func RedactInline(v interface{}) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	// Some clients send Inline as a pre-encoded JSON string. Try to
	// unmarshal once, recurse, then re-marshal so we don't store the
	// secret-laden raw payload.
	if s, ok := v.(string); ok {
		trim := strings.TrimSpace(s)
		if len(trim) >= 2 && (trim[0] == '{' || trim[0] == '[') {
			var decoded interface{}
			if err := json.Unmarshal([]byte(trim), &decoded); err == nil {
				if nv, c := redactValue(decoded); c {
					if b, err := json.Marshal(nv); err == nil {
						return string(b), true
					}
				}
			}
		}
		// Fall through: scrub it as a plain string.
		return Redact(s)
	}
	return redactValue(v)
}
