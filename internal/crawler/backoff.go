package crawler

import (
	"math/rand"
	"time"
)

const (
	defaultMinInterval = 10 * time.Minute
	defaultMaxInterval = 12 * time.Hour
	// maxErrInterval isn't exposed via FR_* env vars (README only tunes the
	// success-path min/max), so it's a fixed ceiling for error backoff.
	maxErrInterval = 24 * time.Hour
)

func clampDuration(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// nextIntervalOnSuccess implements the success-path adaptive interval:
// interval shrinks 30% when new entries showed up (crawl more eagerly),
// grows 30% otherwise (this also covers 304 Not Modified).
func nextIntervalOnSuccess(current time.Duration, newEntries bool, minInterval, maxInterval time.Duration) time.Duration {
	if current <= 0 {
		current = minInterval
	}
	factor := 1.3
	if newEntries {
		factor = 0.7
	}
	return clampDuration(time.Duration(float64(current)*factor), minInterval, maxInterval)
}

// nextIntervalOnError implements the error-path backoff: exponential in the
// (post-increment) error count, capped at maxErrInterval, with up to a
// minute of jitter so many simultaneously-failing feeds don't retry in
// lockstep. A server-supplied Retry-After always wins when present.
func nextIntervalOnError(current time.Duration, errorCountAfter int64, retryAfter, minInterval time.Duration) time.Duration {
	if retryAfter > 0 {
		return clampDuration(retryAfter, 0, maxErrInterval)
	}
	if current <= 0 {
		current = minInterval
	}
	shift := errorCountAfter
	if shift > 8 {
		shift = 8
	}
	if shift < 0 {
		shift = 0
	}
	backoff := current << uint(shift)
	jitter := time.Duration(rand.Int63n(int64(time.Minute)))
	return clampDuration(backoff+jitter, minInterval, maxErrInterval)
}
