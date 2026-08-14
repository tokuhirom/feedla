package store

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// BackupTo writes a consistent snapshot of the database to destPath using
// SQLite's VACUUM INTO, which is safe to run while the database is in WAL
// mode and under concurrent use. It vacuums into a sibling temp file and
// renames it into place on success, so a failed/canceled attempt (e.g. ctx
// expiring mid-VACUUM) never destroys a previously good backup at destPath.
func (s *Store) BackupTo(ctx context.Context, destPath string) error {
	tmpPath := destPath + ".tmp"
	if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: backup to %s: remove stale temp file: %w", destPath, err)
	}
	if _, err := s.Write.ExecContext(ctx, "VACUUM INTO ?", tmpPath); err != nil {
		return fmt.Errorf("store: backup to %s: %w", destPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("store: backup to %s: rename into place: %w", destPath, err)
	}
	return nil
}
