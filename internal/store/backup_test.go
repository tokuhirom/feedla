package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestBackupToWritesConsistentSnapshot(t *testing.T) {
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

	dest := filepath.Join(dir, "backup.db")
	if err := st.BackupTo(ctx, dest); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	backup, err := store.Open(dest)
	if err != nil {
		t.Fatalf("store.Open(backup): %v", err)
	}
	defer backup.Close()

	var count int
	if err := backup.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds`).Scan(&count); err != nil {
		t.Fatalf("query backup: %v", err)
	}
	if count != 1 {
		t.Fatalf("backup feeds count = %d, want 1", count)
	}

	// Backing up again to the same path must not fail even though the
	// destination already exists.
	if err := st.BackupTo(ctx, dest); err != nil {
		t.Fatalf("BackupTo (again): %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("stat dest after second backup: %v", err)
	}
}
