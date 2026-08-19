package maintenance_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/maintenance"
	"github.com/tokuhirom/feedla/internal/store"
)

// completeSetup gives the bootstrap admin (id=1) a real password so the
// store no longer reports setup as pending -- Runner.backup deliberately
// refuses to run before that (see its doc comment), so every test that
// expects a backup to happen needs this.
func completeSetup(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.CompleteSetup(context.Background(), 1, "admin", "not-the-sentinel", time.Now()); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}
}

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
	completeSetup(t, st)

	ctx := context.Background()
	if _, err := st.UpsertFeed(ctx, "https://example.com/feed", "", "", 1800, time.Now()); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	backupDir := filepath.Join(dir, "backup")
	r := maintenance.NewRunner(st, maintenance.Config{
		BackupDir: backupDir,
		Interval:  time.Millisecond,
	})

	// Only needs to outlast scheduling the first tick (Interval is 1ms) --
	// tick's own GC+backup work runs on a context detached from runCtx (see
	// Runner.tick), so a slow tick under `go test ./...`'s package
	// concurrency no longer races this deadline.
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

// fakeRemoteUploader is a maintenance.RemoteUploader that records calls
// in-process instead of talking to real (or mock) object storage --
// internal/remotebackup's own tests already cover the S3 wire protocol
// against gofakes3, so here we only need to verify the Runner calls Store
// with the right key/path for each backup file.
type fakeRemoteUploader struct {
	mu    sync.Mutex
	calls []string // "key=localPath"
}

func (f *fakeRemoteUploader) Store(_ context.Context, key, localPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key+"="+localPath)
	return nil
}

func (f *fakeRemoteUploader) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestRunnerRunUploadsBackupToRemote(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "feedla.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	completeSetup(t, st)

	backupDir := filepath.Join(dir, "backup")
	remote := &fakeRemoteUploader{}
	r := maintenance.NewRunner(st, maintenance.Config{
		BackupDir: backupDir,
		Remote:    remote,
		Interval:  time.Millisecond,
	})

	runCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := r.Run(runCtx); err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	stamp := time.Now().Format("20060102")
	wantDB := "feedla-" + stamp + ".db=" + filepath.Join(backupDir, "feedla-"+stamp+".db")
	wantOPML := "feedla-" + stamp + ".opml=" + filepath.Join(backupDir, "feedla-"+stamp+".opml")

	got := remote.snapshot()
	foundDB, foundOPML := false, false
	for _, call := range got {
		if call == wantDB {
			foundDB = true
		}
		if call == wantOPML {
			foundOPML = true
		}
	}
	if !foundDB || !foundOPML {
		t.Fatalf("remote uploader calls = %v, want to contain %q and %q", got, wantDB, wantOPML)
	}
}

func TestRunnerRun_BacksUpImmediatelyIfTodaysSnapshotMissing(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "feedla.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	completeSetup(t, st)

	backupDir := filepath.Join(dir, "backup")
	r := maintenance.NewRunner(st, maintenance.Config{
		BackupDir: backupDir,
		// Long enough that the regular ticker can't be what produces the
		// backup within the test's short-lived ctx below -- only the
		// startup check (Run, before the ticker loop) can.
		Interval: time.Hour,
	})

	runCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := r.Run(runCtx); err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	stamp := time.Now().Format("20060102")
	if _, err := os.Stat(filepath.Join(backupDir, "feedla-"+stamp+".db")); err != nil {
		t.Fatalf("stat today's backup: %v", err)
	}
}

func TestRunnerRun_SkipsImmediateBackupIfTodaysSnapshotAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "feedla.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	completeSetup(t, st)

	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	stamp := time.Now().Format("20060102")
	existing := filepath.Join(backupDir, "feedla-"+stamp+".db")
	if err := os.WriteFile(existing, []byte("already backed up today"), 0o644); err != nil {
		t.Fatalf("write existing backup: %v", err)
	}

	r := maintenance.NewRunner(st, maintenance.Config{BackupDir: backupDir, Interval: time.Hour})

	runCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := r.Run(runCtx); err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing backup: %v", err)
	}
	if string(got) != "already backed up today" {
		t.Fatalf("existing backup was overwritten, content = %q", got)
	}
}

// TestRunnerRun_SkipsBackupWhileSetupPending pins the guard against the
// "empty DB overwrites/prunes real backups" failure mode: a fresh store
// whose initial setup hasn't happened yet must produce no local snapshot
// and no remote uploads, even though BackupDir and Remote are configured
// and today's snapshot is missing (which normally triggers an immediate
// startup backup).
func TestRunnerRun_SkipsBackupWhileSetupPending(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "feedla.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	backupDir := filepath.Join(dir, "backup")
	remote := &fakeRemoteUploader{}
	r := maintenance.NewRunner(st, maintenance.Config{
		BackupDir: backupDir,
		Remote:    remote,
		Interval:  time.Millisecond,
	})

	runCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := r.Run(runCtx); err != context.DeadlineExceeded {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	if entries, err := os.ReadDir(backupDir); err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("backupDir = %v, want no snapshots while setup is pending", names)
	}
	if calls := remote.snapshot(); len(calls) > 0 {
		t.Fatalf("remote uploader calls = %v, want none while setup is pending", calls)
	}
}
