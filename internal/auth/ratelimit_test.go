package auth_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
)

func TestLoginLimiterIPWindow(t *testing.T) {
	l := auth.NewLoginLimiter(3, time.Minute)

	// Each attempt uses a distinct username so only the IP-level window is
	// exercised here (a repeated username would also trip the per-account
	// backoff, conflating the two mechanisms).
	for i := range 3 {
		username := fmt.Sprintf("user%d", i)
		if !l.Allow(username, "1.2.3.4") {
			t.Fatalf("attempt %d: expected Allow", i)
		}
		l.RecordFailure(username, "1.2.3.4")
	}
	if l.Allow("bob", "1.2.3.4") {
		t.Fatal("expected IP window to block a 4th attempt from the same IP, even for a different account")
	}
}

func TestLoginLimiterAccountBackoff(t *testing.T) {
	l := auth.NewLoginLimiter(1000, time.Minute)

	if !l.Allow("alice", "1.2.3.4") {
		t.Fatal("first attempt should be allowed")
	}
	l.RecordFailure("alice", "1.2.3.4")

	if l.Allow("alice", "5.6.7.8") {
		t.Fatal("expected account backoff to block immediate retry even from a different IP")
	}

	// A different account is unaffected by alice's backoff.
	if !l.Allow("carol", "5.6.7.8") {
		t.Fatal("a different account should not be blocked by alice's backoff")
	}
}

func TestLoginLimiterRecordSuccessClearsBackoff(t *testing.T) {
	l := auth.NewLoginLimiter(1000, time.Minute)

	l.RecordFailure("alice", "1.2.3.4")
	if l.Allow("alice", "1.2.3.4") {
		t.Fatal("expected backoff after a failure")
	}
	l.RecordSuccess("alice")
	if !l.Allow("alice", "1.2.3.4") {
		t.Fatal("expected RecordSuccess to clear the account backoff")
	}
}
