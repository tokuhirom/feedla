package restore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tokuhirom/feedla/internal/restore"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestIfMissing_NoOpWhenDBAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")
	writeFile(t, dbPath, "live-db")

	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, "feedla-20260101.db"), "should-not-be-used")

	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{BackupDir: backupDir}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "live-db" {
		t.Fatalf("dbPath content = %q, want unchanged %q", got, "live-db")
	}
}

func TestIfMissing_RestoresLatestLocalBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")

	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, "feedla-20260101.db"), "older")
	writeFile(t, filepath.Join(backupDir, "feedla-20260215.db"), "newest")
	writeFile(t, filepath.Join(backupDir, "feedla-20260110.db"), "middle")

	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{BackupDir: backupDir}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "newest" {
		t.Fatalf("dbPath content = %q, want %q", got, "newest")
	}
}

func TestIfMissing_FallsBackToRemoteWhenLocalHasNothing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")

	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	remote := &fakeDownloader{key: "feedla/feedla-20260301.db", content: "from-remote"}
	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{BackupDir: backupDir, Remote: remote}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "from-remote" {
		t.Fatalf("dbPath content = %q, want %q", got, "from-remote")
	}
	if remote.downloadedKey != "feedla/feedla-20260301.db" {
		t.Fatalf("downloaded key = %q, want %q", remote.downloadedKey, "feedla/feedla-20260301.db")
	}
}

func TestIfMissing_NoBackupAnywhereIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")

	remote := &fakeDownloader{}
	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{Remote: remote}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dbPath should still not exist, stat err = %v", err)
	}
}

type fakeDownloader struct {
	key           string
	content       string
	listErr       error
	downloadedKey string
}

func (f *fakeDownloader) Latest(_ context.Context, _ string) (string, bool, error) {
	if f.listErr != nil {
		return "", false, f.listErr
	}
	if f.key == "" {
		return "", false, nil
	}
	return f.key, true, nil
}

func (f *fakeDownloader) Download(_ context.Context, key, destPath string) error {
	f.downloadedKey = key
	return os.WriteFile(destPath, []byte(f.content), 0o644)
}

// TestIfMissing_PrefersNewerRemoteOverStaleLocal pins the newest-wins rule:
// a leftover local snapshot must not shadow a more recent remote one (the
// old local-first behavior could restore stale data and then let the daily
// backup overwrite today's good remote snapshot with it).
func TestIfMissing_PrefersNewerRemoteOverStaleLocal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")

	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, "feedla-20260101.db"), "stale-local")

	remote := &fakeDownloader{key: "feedla/feedla-20260301.db", content: "newer-remote"}
	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{BackupDir: backupDir, Remote: remote}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "newer-remote" {
		t.Fatalf("dbPath content = %q, want %q", got, "newer-remote")
	}
}

func TestIfMissing_PrefersNewerLocalOverOlderRemote(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")

	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, "feedla-20260301.db"), "newer-local")

	remote := &fakeDownloader{key: "feedla/feedla-20260101.db", content: "older-remote"}
	if err := restore.IfMissing(context.Background(), dbPath, restore.Config{BackupDir: backupDir, Remote: remote}); err != nil {
		t.Fatalf("IfMissing: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "newer-local" {
		t.Fatalf("dbPath content = %q, want %q", got, "newer-local")
	}
}

// A broken remote must not block a restore that a local snapshot can
// satisfy -- but with no local candidate the error has to surface, since
// silently starting fresh is exactly the failure mode this package exists
// to avoid.
func TestLatest_RemoteErrorFallsBackToLocal(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(backupDir, "feedla-20260101.db"), "local")

	remote := &fakeDownloader{listErr: errors.New("bucket unreachable")}
	snap, found, err := restore.Latest(context.Background(), restore.Config{BackupDir: backupDir, Remote: remote})
	if err != nil || !found {
		t.Fatalf("Latest = (%+v, %v, %v), want local snapshot found", snap, found, err)
	}
	if snap.Source != "local" || snap.Base() != "feedla-20260101.db" {
		t.Fatalf("snap = %+v, want local feedla-20260101.db", snap)
	}

	if _, _, err := restore.Latest(context.Background(), restore.Config{Remote: remote}); err == nil {
		t.Fatalf("Latest with only a broken remote should return its error")
	}
}

func TestPromoteStaged_ReplacesDBAndSidecars(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")
	staged := dbPath + ".restore"

	writeFile(t, dbPath, "old-live-db")
	writeFile(t, dbPath+"-wal", "old-wal")
	writeFile(t, dbPath+"-shm", "old-shm")
	writeFile(t, staged, "restored")

	if err := restore.PromoteStaged(staged, dbPath); err != nil {
		t.Fatalf("PromoteStaged: %v", err)
	}

	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read dbPath: %v", err)
	}
	if string(got) != "restored" {
		t.Fatalf("dbPath content = %q, want %q", got, "restored")
	}
	for _, p := range []string{staged, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s should be gone, stat err = %v", p, err)
		}
	}
}

func TestPromoteStaged_ErrorsWhenNothingStaged(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "feedla.db")
	writeFile(t, dbPath, "live-db")

	if err := restore.PromoteStaged(dbPath+".restore", dbPath); err == nil {
		t.Fatalf("PromoteStaged with no staged file should error")
	}
	got, _ := os.ReadFile(dbPath)
	if string(got) != "live-db" {
		t.Fatalf("dbPath content = %q, want untouched %q", got, "live-db")
	}
}
