package auth

import (
	"sync"
	"time"
)

// ActionLimiter is a generic per-key fixed-window rate limiter, used for
// the FR_QUOTA_* action rate limits from docs/multi-user-design.md's
// リソース制限・abuse 対策 section (feed add/hour, manual refresh/hour,
// pagewatch preview/hour, API requests/minute). Keys are caller-defined
// (typically "<userID>:<action>" or "<userID>" for the API-wide limit),
// which keeps one limiter type usable for every quota below.
//
// Like LoginLimiter, state is in-memory only and resets on restart; small
// deployments accept that tradeoff. Unlike LoginLimiter, the window is
// fixed-size per key, not tied to a specific username/IP shape, since the
// action quotas don't need the two-tier account+IP design login abuse
// protection does.
type ActionLimiter struct {
	mu sync.Mutex

	windows map[string]*fixedWindow

	limit  int
	window time.Duration
	now    func() time.Time
}

type fixedWindow struct {
	start time.Time
	count int
}

// NewActionLimiter builds a limiter allowing up to limit calls per window,
// per key. A non-positive limit disables the limit (Allow always true).
func NewActionLimiter(limit int, window time.Duration) *ActionLimiter {
	return &ActionLimiter{
		windows: make(map[string]*fixedWindow),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// Allow reports whether key may proceed right now, and if so, records the
// call against key's current window. There's no separate Record step --
// callers only need to check quota, not distinguish success/failure like
// login attempts do.
func (l *ActionLimiter) Allow(key string) bool {
	if l.limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	w, ok := l.windows[key]
	if !ok {
		w = &fixedWindow{start: now}
		l.windows[key] = w
	}
	if now.Sub(w.start) > l.window {
		w.start = now
		w.count = 0
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
