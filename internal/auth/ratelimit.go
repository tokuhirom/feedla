package auth

import (
	"sync"
	"time"
)

// LoginLimiter implements the two-tier login rate limit from
// docs/multi-user-design.md's ブルートフォース対策 section:
//   - per-account: exponential backoff on consecutive failures (1s, 2s,
//     4s, ... capped at 15m). A full lockout is deliberately not used,
//     since that would let an attacker lock a legitimate user out by
//     repeatedly failing their login.
//   - per-IP: a fixed window (default 10 requests/minute), to slow down
//     an attacker trying many usernames from one address.
//
// Both are kept in memory only; state resets on process restart, which
// docs/multi-user-design.md accepts as fine for a small-scale deployment.
type LoginLimiter struct {
	mu sync.Mutex

	accounts map[string]*accountState
	ips      map[string]*ipState

	ipLimit  int
	ipWindow time.Duration
	maxBack  time.Duration

	now func() time.Time
}

type accountState struct {
	failures  int
	nextAllow time.Time
}

type ipState struct {
	windowStart time.Time
	count       int
}

// NewLoginLimiter builds a limiter with the given per-IP request budget
// (ipLimit requests per ipWindow).
func NewLoginLimiter(ipLimit int, ipWindow time.Duration) *LoginLimiter {
	return &LoginLimiter{
		accounts: make(map[string]*accountState),
		ips:      make(map[string]*ipState),
		ipLimit:  ipLimit,
		ipWindow: ipWindow,
		maxBack:  15 * time.Minute,
		now:      time.Now,
	}
}

// Allow reports whether a login attempt for username from clientIP may
// proceed right now. It does not record the attempt outcome -- call
// RecordFailure or RecordSuccess after the password check.
func (l *LoginLimiter) Allow(username, clientIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	if ip, ok := l.ips[clientIP]; ok {
		if now.Sub(ip.windowStart) > l.ipWindow {
			ip.windowStart = now
			ip.count = 0
		}
		if ip.count >= l.ipLimit {
			return false
		}
	}

	if acc, ok := l.accounts[username]; ok && now.Before(acc.nextAllow) {
		return false
	}

	return true
}

// RecordFailure registers a failed login attempt, advancing both the
// account's backoff and the IP's request count.
func (l *LoginLimiter) RecordFailure(username, clientIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	ip, ok := l.ips[clientIP]
	if !ok {
		ip = &ipState{windowStart: now}
		l.ips[clientIP] = ip
	}
	if now.Sub(ip.windowStart) > l.ipWindow {
		ip.windowStart = now
		ip.count = 0
	}
	ip.count++

	acc, ok := l.accounts[username]
	if !ok {
		acc = &accountState{}
		l.accounts[username] = acc
	}
	acc.failures++
	backoff := min(time.Duration(1<<min(acc.failures, 20))*time.Second, l.maxBack)
	acc.nextAllow = now.Add(backoff)
}

// RecordSuccess clears the account's failure count after a successful
// login. The IP counter is left as-is: it exists to slow down username
// enumeration/spraying, not to punish a single legitimate user.
func (l *LoginLimiter) RecordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.accounts, username)
}
