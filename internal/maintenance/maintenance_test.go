package maintenance_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/maintenance"
	"github.com/tokuhirom/feedla/internal/store"
)

func TestRunnerRunStopsOnContextCancel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	r := maintenance.NewRunner(st, maintenance.Config{
		RetentionDays:    30,
		RetentionPerFeed: 1000,
		Interval:         time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err = r.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunnerRunDeletesExpiredSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.Now()
	if _, err := st.CreateSession(context.Background(), 1, []byte("expired"), now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	r := maintenance.NewRunner(st, maintenance.Config{Interval: time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = r.Run(ctx)

	if _, err := st.GetSessionByTokenHash(context.Background(), []byte("expired")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session should have been deleted by maintenance tick, err = %v", err)
	}
}

func TestRunnerRunWritesBackup(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "feedla.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := st.UpsertFeed(ctx, "https://example.com/feed", "", "", 1800, time.Now()); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	backupDir := filepath.Join(dir, "backup")
	r := maintenance.NewRunner(st, maintenance.Config{
		BackupDir: backupDir,
		Interval:  time.Millisecond,
	})

	// Generous relative to Interval: under `go test ./...` many packages'
	// test binaries run concurrently and can starve this goroutine for a
	// while, so the deadline needs real headroom past the first tick.
	runCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := r.Run(runCtx); err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	stamp := time.Now().Format("20060102")
	for _, name := range []string{"feedla-" + stamp + ".db", "feedla-" + stamp + ".opml"} {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
}
