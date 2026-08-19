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
	downloadedKey string
}

func (f *fakeDownloader) Latest(_ context.Context, _ string) (string, bool, error) {
	if f.key == "" {
		return "", false, nil
	}
	return f.key, true, nil
}

func (f *fakeDownloader) Download(_ context.Context, key, destPath string) error {
	f.downloadedKey = key
	return os.WriteFile(destPath, []byte(f.content), 0o644)
}
