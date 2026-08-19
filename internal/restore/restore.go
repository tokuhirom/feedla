// Package restore recovers feedla's DB file from the most recent available
// backup when the file feedla is about to open doesn't exist yet -- e.g. a
// fresh deploy target, or a volume that lost the live DB but kept (or can
// reach) its backups.
package restore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// Downloader fetches a database snapshot from off-host storage, e.g.
// *internal/remotebackup.Client. Latest reports the most recent available
// object key ending in ext ("found" is false if none exist); Download
// fetches that key to destPath.
type Downloader interface {
	Latest(ctx context.Context, ext string) (key string, found bool, err error)
	Download(ctx context.Context, key, destPath string) error
}

// Config controls where IfMissing looks for a database snapshot to restore
// from. BackupDir == "" skips the local lookup; Remote == nil skips the
// off-host lookup.
type Config struct {
	BackupDir string
	Remote    Downloader
}

// IfMissing restores dbPath from the most recent available backup if dbPath
// doesn't already exist yet. It checks cfg.BackupDir first (the daily
// feedla-YYYYMMDD.db snapshot written by internal/maintenance), falling
// back to cfg.Remote if the local directory has nothing. It's a no-op if
// dbPath already exists, or if no backup is found anywhere -- in the latter
// case the caller's subsequent store.Open just creates a fresh DB, as
// before this package existed.
func IfMissing(ctx context.Context, dbPath string, cfg Config) error {
	if _, err := os.Stat(dbPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("restore: stat %s: %w", dbPath, err)
	}

	if cfg.BackupDir != "" {
		path, found, err := latestLocal(cfg.BackupDir)
		if err != nil {
			return fmt.Errorf("restore: scan %s: %w", cfg.BackupDir, err)
		}
		if found {
			slog.Info("restore: restoring db from local backup", "path", path, "dest", dbPath)
			return copyFile(path, dbPath)
		}
	}

	if cfg.Remote != nil {
		key, found, err := cfg.Remote.Latest(ctx, ".db")
		if err != nil {
			return fmt.Errorf("restore: list remote backups: %w", err)
		}
		if found {
			slog.Info("restore: restoring db from remote backup", "key", key, "dest", dbPath)
			return cfg.Remote.Download(ctx, key, dbPath)
		}
	}

	slog.Info("restore: no backup found, starting with a fresh database", "dest", dbPath)
	return nil
}

// latestLocal returns the most recent feedla-YYYYMMDD.db snapshot in dir, or
// found=false if dir doesn't exist or contains none. Filenames embed a
// sortable date, so the lexicographically largest match is also the most
// recent (same convention as internal/remotebackup's pruning/Latest).
func latestLocal(dir string) (path string, found bool, err error) {
	matches, err := filepath.Glob(filepath.Join(dir, "feedla-*.db"))
	if err != nil {
		return "", false, err
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	sort.Strings(matches)
	return matches[len(matches)-1], true, nil
}

// copyFile copies src to dest via a sibling temp file + rename, so a
// failed/canceled copy never leaves a truncated file at dest.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("restore: open %s: %w", src, err)
	}
	defer in.Close()

	tmpPath := dest + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("restore: create %s: %w", tmpPath, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("restore: copy %s to %s: %w", src, tmpPath, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("restore: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("restore: rename into place: %w", err)
	}
	return nil
}
