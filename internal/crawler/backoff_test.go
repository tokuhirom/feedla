package crawler

import (
	"testing"
	"time"
)

func TestNextIntervalOnSuccess(t *testing.T) {
	min, max := 10*time.Minute, 12*time.Hour

	if got := nextIntervalOnSuccess(time.Hour, true, min, max); got != 42*time.Minute {
		t.Errorf("new entries: got %v, want %v (interval*0.7)", got, 42*time.Minute)
	}
	if got := nextIntervalOnSuccess(time.Hour, false, min, max); got != 78*time.Minute {
		t.Errorf("no new entries: got %v, want %v (interval*1.3)", got, 78*time.Minute)
	}
	if got := nextIntervalOnSuccess(11*time.Minute, true, min, max); got < min {
		t.Errorf("shrinking interval = %v, must not go below min %v", got, min)
	}
	if got := nextIntervalOnSuccess(11*time.Hour, false, min, max); got > max {
		t.Errorf("growing interval = %v, must not exceed max %v", got, max)
	}
}

func TestNextIntervalOnError(t *testing.T) {
	min := 10 * time.Minute

	got := nextIntervalOnError(time.Hour, 1, 0, min)
	if got < 2*time.Hour || got > 2*time.Hour+time.Minute {
		t.Errorf("first error backoff = %v, want ~2h (interval*2 + <=1m jitter)", got)
	}

	// error_count is clamped to a shift of 8, so it must never exceed maxErrInterval.
	got = nextIntervalOnError(12*time.Hour, 100, 0, min)
	if got > maxErrInterval {
		t.Errorf("capped backoff = %v, want <= maxErrInterval %v", got, maxErrInterval)
	}

	// Retry-After always wins over the computed backoff.
	if got := nextIntervalOnError(time.Hour, 5, 90*time.Second, min); got != 90*time.Second {
		t.Errorf("Retry-After override: got %v, want 90s", got)
	}
}
