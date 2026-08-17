package auth_test

import (
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
)

func TestActionLimiterBlocksAfterLimit(t *testing.T) {
	l := auth.NewActionLimiter(3, time.Minute)

	for i := range 3 {
		if !l.Allow("user1") {
			t.Fatalf("attempt %d: expected Allow", i)
		}
	}
	if l.Allow("user1") {
		t.Fatal("expected the 4th attempt within the window to be blocked")
	}
}

func TestActionLimiterKeysAreIndependent(t *testing.T) {
	l := auth.NewActionLimiter(1, time.Minute)

	if !l.Allow("user1") {
		t.Fatal("first attempt for user1 should be allowed")
	}
	if l.Allow("user1") {
		t.Fatal("second attempt for user1 should be blocked")
	}
	if !l.Allow("user2") {
		t.Fatal("user2 should be unaffected by user1's quota")
	}
}

func TestActionLimiterNonPositiveLimitDisabled(t *testing.T) {
	l := auth.NewActionLimiter(0, time.Minute)
	for i := range 100 {
		if !l.Allow("user1") {
			t.Fatalf("attempt %d: a non-positive limit should never block", i)
		}
	}
}
