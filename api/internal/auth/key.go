// Package auth handles API-key generation, hashing and request-scoped
// tenant resolution for the audit log API.
package auth

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// KeyPrefixLive is the default prefix for new Fact0 API keys.
const KeyPrefixLive = "f0_live_"

// KeyPrefixLegacy is the legacy Fact0 prefix (still accepted).
const KeyPrefixLegacy = "alk_live_"

// KeyPrefix is an alias for KeyPrefixLive (new keys).
const KeyPrefix = KeyPrefixLive

const rawSecretBytes = 32

var keyPrefixes = []string{KeyPrefixLive, KeyPrefixLegacy}

// GenerateKey returns a fresh, raw API key and its bcrypt hash.
// The returned string `token` is what the user sees (`f0_live_<ID>_<SECRET>`).
// The returned string `hash` is the bcrypt output to store in the DB.
func GenerateKey(id string) (token string, hash string, err error) {
	b := make([]byte, rawSecretBytes)
	if _, err := cryptorand.Read(b); err != nil {
		return "", "", fmt.Errorf("reading entropy: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(b)
	token = KeyPrefixLive + id + "." + secret

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return token, string(hashedBytes), nil
}

// Hash is retained for tests or legacy fallback if needed, but new keys use bcrypt.
func Hash(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// VerifyAPIKey parses the token and verifies the secret against the stored bcrypt hash.
func VerifyAPIKey(token string, storedHash string) (id string, ok bool) {
	if !strings.HasPrefix(token, KeyPrefixLive) {
		return "", false
	}
	stripped := strings.TrimPrefix(token, KeyPrefixLive)
	parts := strings.SplitN(stripped, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	id = parts[0]
	secret := parts[1]
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(secret))
	return id, err == nil
}

// LooksLikeKey returns true if s matches a supported API key prefix.
func LooksLikeKey(s string) bool {
	for _, p := range keyPrefixes {
		if strings.HasPrefix(s, p) && len(s) >= len(p)+10 {
			return true
		}
	}
	return false
}
