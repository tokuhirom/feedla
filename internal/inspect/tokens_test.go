package inspect

import (
	"testing"
	"time"
)

func TestTokenStoreIssueThenConsumeRoundTrips(t *testing.T) {
	s := NewTokenStore()
	now := time.Now()

	token, err := s.Issue(42, []byte("<p>hi</p>"), []Element{{ID: 1, Tag: "p"}}, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned an empty token")
	}

	entry, ok := s.Consume(token, now)
	if !ok {
		t.Fatal("Consume(fresh token) = false, want true")
	}
	if entry.UserID != 42 || string(entry.HTML) != "<p>hi</p>" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestTokenStoreConsumeIsSingleUse(t *testing.T) {
	s := NewTokenStore()
	now := time.Now()
	token, err := s.Issue(1, []byte("x"), nil, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, ok := s.Consume(token, now); !ok {
		t.Fatal("first Consume = false, want true")
	}
	if _, ok := s.Consume(token, now); ok {
		t.Fatal("second Consume = true, want false (single use)")
	}
}

func TestTokenStoreConsumeUnknownTokenFails(t *testing.T) {
	s := NewTokenStore()
	if _, ok := s.Consume("does-not-exist", time.Now()); ok {
		t.Fatal("Consume(unknown token) = true, want false")
	}
}

func TestTokenStoreConsumeAfterExpiryFails(t *testing.T) {
	s := NewTokenStore()
	issuedAt := time.Now()
	token, err := s.Issue(1, []byte("x"), nil, issuedAt)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, ok := s.Consume(token, issuedAt.Add(TTL+time.Second)); ok {
		t.Fatal("Consume(expired token) = true, want false")
	}
}

func TestTokenStoreDoesNotFilterByUser(t *testing.T) {
	// Consume has no userID parameter by design (see its doc comment): the
	// token itself is the sole authorization, since the legitimate caller
	// -- a sandboxed iframe -- may not have a session at all. This test
	// exists to make that contract explicit and regression-proof, not to
	// suggest weakening it.
	s := NewTokenStore()
	now := time.Now()
	token, err := s.Issue(1, []byte("x"), nil, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	entry, ok := s.Consume(token, now)
	if !ok {
		t.Fatal("Consume = false, want true regardless of caller identity")
	}
	if entry.UserID != 1 {
		t.Fatalf("entry.UserID = %d, want 1 (available for an optional caller-side check)", entry.UserID)
	}
}

func TestTokenStoreSweepsExpiredEntriesOnIssueAndConsume(t *testing.T) {
	s := NewTokenStore()
	t0 := time.Now()

	if _, err := s.Issue(1, []byte("stale"), nil, t0); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	afterExpiry := t0.Add(TTL + time.Second)
	// Issuing a second token after the first has expired should sweep it.
	if _, err := s.Issue(2, []byte("fresh"), nil, afterExpiry); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(s.entries) != 1 {
		t.Fatalf("expected the expired entry to be swept on Issue, got %d entries", len(s.entries))
	}

	// Consuming an unrelated/unknown token should also sweep anything
	// expired in the meantime.
	fresh2, err := s.Issue(3, []byte("fresh2"), nil, afterExpiry)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	muchLater := afterExpiry.Add(TTL + time.Second)
	s.Consume("unknown-token", muchLater)
	if _, ok := s.entries[fresh2]; ok {
		t.Fatal("expected fresh2 to be swept by an unrelated Consume call after its own expiry")
	}
}
