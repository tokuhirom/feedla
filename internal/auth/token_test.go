package auth_test

import (
	"bytes"
	"testing"

	"github.com/tokuhirom/feedla/internal/auth"
)

func TestGenerateTokenUniqueAndHashMatches(t *testing.T) {
	raw1, hash1, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	raw2, hash2, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	if raw1 == raw2 {
		t.Fatal("two generated tokens should differ")
	}
	if bytes.Equal(hash1, hash2) {
		t.Fatal("hashes of two different tokens should differ")
	}
	if !bytes.Equal(auth.HashToken(raw1), hash1) {
		t.Fatal("HashToken(raw1) should match the hash returned alongside it")
	}
}
