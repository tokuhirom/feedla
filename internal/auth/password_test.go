package auth_test

import (
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=") {
		t.Fatalf("hash = %q, want $argon2id$ prefix", hash)
	}

	ok, err := auth.VerifyPassword(hash, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword: correct password rejected")
	}

	ok, err = auth.VerifyPassword(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword: wrong password accepted")
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	_, err := auth.VerifyPassword("!locked!", "anything")
	if err != auth.ErrInvalidHash {
		t.Fatalf("err = %v, want ErrInvalidHash", err)
	}
}

func TestVerifyDummyPasswordDoesNotPanic(t *testing.T) {
	auth.VerifyDummyPassword("whatever")
}

func TestNeedsRehash(t *testing.T) {
	hash, err := auth.HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if auth.NeedsRehash(hash) {
		t.Fatal("freshly hashed password should not need rehash")
	}
	// An unparseable hash (e.g. the '!locked!' sentinel) is reported as not
	// needing rehash: NeedsRehash is only ever called after a successful
	// VerifyPassword, which already rejects unparseable hashes, so there's
	// no plaintext available to rehash with here.
	if auth.NeedsRehash("!locked!") {
		t.Fatal("unparseable hash should not be reported as needing rehash")
	}
}

func TestHashPasswordUniqueSalt(t *testing.T) {
	h1, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("two hashes of the same password should differ (random salt)")
	}
}
