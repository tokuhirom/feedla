package crawler

import (
	"context"
	"sync"
	"time"
)

const (
	defaultHostConcurrency = 2
	defaultHostMinGap      = time.Second
)

// HostSemaphore enforces feedla's per-host politeness rules: at most
// maxPerHost concurrent requests to a given host, and at least minGap
// between the starts of two requests to that host.
type HostSemaphore struct {
	mu         sync.Mutex
	slots      map[string]chan struct{}
	lastStart  map[string]time.Time
	maxPerHost int
	minGap     time.Duration
}

// NewHostSemaphore builds a HostSemaphore. maxPerHost <= 0 falls back to the
// README default of 2.
func NewHostSemaphore(maxPerHost int, minGap time.Duration) *HostSemaphore {
	if maxPerHost <= 0 {
		maxPerHost = defaultHostConcurrency
	}
	return &HostSemaphore{
		slots:      make(map[string]chan struct{}),
		lastStart:  make(map[string]time.Time),
		maxPerHost: maxPerHost,
		minGap:     minGap,
	}
}

func (h *HostSemaphore) chanFor(host string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.slots[host]
	if !ok {
		ch = make(chan struct{}, h.maxPerHost)
		h.slots[host] = ch
	}
	return ch
}

// Acquire blocks until a concurrency slot for host is free and at least
// minGap has elapsed since the last request to host started, then reserves
// the slot. The caller must invoke the returned release func exactly once
// when the request is done (success or failure).
func (h *HostSemaphore) Acquire(ctx context.Context, host string) (func(), error) {
	ch := h.chanFor(host)
	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	h.mu.Lock()
	wait := h.minGap - time.Since(h.lastStart[host])
	h.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			<-ch
			return nil, ctx.Err()
		}
	}

	h.mu.Lock()
	h.lastStart[host] = time.Now()
	h.mu.Unlock()

	return func() { <-ch }, nil
}

// GC drops bookkeeping for hosts that are currently idle (no reserved
// slots) and haven't been used within idleAfter, so a long-running daemon
// that crawls many distinct hosts over its lifetime doesn't grow these maps
// without bound.
func (h *HostSemaphore) GC(idleAfter time.Duration) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for host, ch := range h.slots {
		if len(ch) == 0 && now.Sub(h.lastStart[host]) > idleAfter {
			delete(h.slots, host)
			delete(h.lastStart, host)
		}
	}
}
