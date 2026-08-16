package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the raw entropy used for session and API tokens: 256 bits,
// per docs/multi-user-design.md's セッション設計 section.
const tokenBytes = 32

// GenerateToken returns a fresh random token (base64url, no padding) plus
// the SHA-256 hash that should be stored in place of the raw value --
// sessions and api_tokens both store only the hash, so a leaked DB or
// backup can't be used to impersonate a session.
func GenerateToken() (raw string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the SHA-256 hash of a raw token, as stored in
// sessions.token_hash / api_tokens.token_hash.
func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func b64Encode(b []byte) string {
	return base64.RawStdEncoding.EncodeToString(b)
}

func b64Decode(s string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(s)
}
