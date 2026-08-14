package store

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// BackupTo writes a consistent snapshot of the database to destPath using
// SQLite's VACUUM INTO, which is safe to run while the database is in WAL
// mode and under concurrent use. destPath must not already exist (a SQLite
// requirement); any existing file there is removed first so repeated runs
// against the same path don't fail.
func (s *Store) BackupTo(ctx context.Context, destPath string) error {
	if err := os.Remove(destPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: backup to %s: remove existing: %w", destPath, err)
	}
	if _, err := s.Write.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("store: backup to %s: %w", destPath, err)
	}
	return nil
}
