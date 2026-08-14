package crawler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostSemaphoreLimitsConcurrency(t *testing.T) {
	h := NewHostSemaphore(2, 0)
	ctx := context.Background()

	var current, maxSeen int64
	acquireAndHold := func(d time.Duration) func() {
		release, err := h.Acquire(ctx, "example.com")
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		n := atomic.AddInt64(&current, 1)
		for {
			old := atomic.LoadInt64(&maxSeen)
			if n <= old || atomic.CompareAndSwapInt64(&maxSeen, old, n) {
				break
			}
		}
		done := make(chan struct{})
		go func() {
			time.Sleep(d)
			atomic.AddInt64(&current, -1)
			release()
			close(done)
		}()
		return func() { <-done }
	}

	wait1 := acquireAndHold(50 * time.Millisecond)
	wait2 := acquireAndHold(50 * time.Millisecond)

	// A third acquire must block until one of the first two releases.
	start := time.Now()
	release3, err := h.Acquire(ctx, "example.com")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("third Acquire returned after %v, want it to block until a slot freed", elapsed)
	}
	release3()

	wait1()
	wait2()
	if got := atomic.LoadInt64(&maxSeen); got > 2 {
		t.Errorf("max concurrent holders = %d, want <= 2", got)
	}
}

func TestHostSemaphoreEnforcesMinGap(t *testing.T) {
	h := NewHostSemaphore(4, 40*time.Millisecond)
	ctx := context.Background()

	release1, err := h.Acquire(ctx, "example.com")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	release1()

	start := time.Now()
	release2, err := h.Acquire(ctx, "example.com")
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	release2()
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("second Acquire returned after %v, want it to wait out minGap", elapsed)
	}
}

func TestHostSemaphoreDifferentHostsDontBlockEachOther(t *testing.T) {
	h := NewHostSemaphore(1, time.Hour) // huge gap: would hang the test if hosts shared state
	ctx := context.Background()

	releaseA, err := h.Acquire(ctx, "a.example.com")
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	defer releaseA()

	done := make(chan struct{})
	go func() {
		releaseB, err := h.Acquire(ctx, "b.example.com")
		if err != nil {
			t.Errorf("Acquire b: %v", err)
			return
		}
		releaseB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Acquire for a different host blocked; per-host state must be independent")
	}
}

func TestHostSemaphoreGC(t *testing.T) {
	h := NewHostSemaphore(2, 0)
	release, err := h.Acquire(context.Background(), "idle.example.com")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()

	h.mu.Lock()
	h.lastStart["idle.example.com"] = time.Now().Add(-time.Hour)
	h.mu.Unlock()

	h.GC(time.Minute)

	h.mu.Lock()
	_, stillThere := h.slots["idle.example.com"]
	h.mu.Unlock()
	if stillThere {
		t.Error("GC did not remove idle host bookkeeping")
	}
}
