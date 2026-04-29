// Package auth contains the bearer-token helpers and gRPC interceptors
// shared by all quicktun authentication paths.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes is the random byte length per session token before encoding.
// 32 bytes ≈ 256 bits of entropy, plenty for a bearer secret.
const tokenBytes = 32

// IssueToken returns a freshly generated session token (raw, ready to hand
// to the client) and its SHA-256 hex hash (for storage). The raw token is
// URL-safe base64 (no padding, no '+' or '/'). Callers must persist the hash
// only — the raw value is shown to the client exactly once at issue time.
func IssueToken() (raw, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("auth: random read: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the SHA-256 hex hash of a raw token. Use this to look up
// a token in storage given the value sent by a client; storing only the hash
// means a leaked database does not directly leak active tokens.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
