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
	"path"
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

// Snapshot identifies one restorable DB snapshot found by Latest: either a
// file under Config.BackupDir (Source "local", Ref is its path) or an
// object in off-host storage (Source "remote", Ref is its key, already
// including any prefix, as returned by Downloader.Latest).
type Snapshot struct {
	Source string // "local" or "remote"
	Ref    string
}

// Base returns the snapshot's bare file name (feedla-YYYYMMDD.db),
// regardless of whether Ref is a filesystem path or an object key.
func (s Snapshot) Base() string {
	if s.Source == "remote" {
		return path.Base(s.Ref)
	}
	return filepath.Base(s.Ref)
}

// Latest returns the newest DB snapshot across cfg.BackupDir and
// cfg.Remote, comparing base file names (which embed a sortable
// YYYYMMDD date) so a stale local snapshot can't shadow a newer remote
// one. A remote listing failure is only fatal when there's no local
// candidate to fall back to -- an unreachable bucket shouldn't block a
// restore that local backups can satisfy (it's logged either way, and the
// setup screen's restore hint reports remote errors separately).
func Latest(ctx context.Context, cfg Config) (Snapshot, bool, error) {
	var best Snapshot
	var found bool

	if cfg.BackupDir != "" {
		p, ok, err := latestLocal(cfg.BackupDir)
		if err != nil {
			return Snapshot{}, false, fmt.Errorf("restore: scan %s: %w", cfg.BackupDir, err)
		}
		if ok {
			best = Snapshot{Source: "local", Ref: p}
			found = true
		}
	}

	if cfg.Remote != nil {
		key, ok, err := cfg.Remote.Latest(ctx, ".db")
		switch {
		case err != nil && !found:
			return Snapshot{}, false, fmt.Errorf("restore: list remote backups: %w", err)
		case err != nil:
			slog.Warn("restore: listing remote backups failed, falling back to local", "error", err)
		case ok:
			remote := Snapshot{Source: "remote", Ref: key}
			if !found || remote.Base() > best.Base() {
				best = remote
				found = true
			}
		}
	}

	return best, found, nil
}

// Fetch writes snap's contents to destPath. Both paths write via a sibling
// temp file + rename, so a failed/canceled fetch never leaves a truncated
// file at destPath.
func Fetch(ctx context.Context, cfg Config, snap Snapshot, destPath string) error {
	if snap.Source == "remote" {
		return cfg.Remote.Download(ctx, snap.Ref, destPath)
	}
	return copyFile(snap.Ref, destPath)
}

// IfMissing restores dbPath from the newest available backup (local or
// remote, whichever is more recent -- see Latest) if dbPath doesn't
// already exist yet. It's a no-op if dbPath already exists, or if no
// backup is found anywhere -- in the latter case the caller's subsequent
// store.Open just creates a fresh DB, as before this package existed.
func IfMissing(ctx context.Context, dbPath string, cfg Config) error {
	if _, err := os.Stat(dbPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("restore: stat %s: %w", dbPath, err)
	}

	snap, found, err := Latest(ctx, cfg)
	if err != nil {
		return err
	}
	if !found {
		slog.Info("restore: no backup found, starting with a fresh database", "dest", dbPath)
		return nil
	}

	slog.Info("restore: restoring db from backup", "source", snap.Source, "ref", snap.Ref, "dest", dbPath)
	return Fetch(ctx, cfg, snap, dbPath)
}

// PromoteStaged moves a snapshot staged by Fetch into place as the live DB
// file, first clearing dbPath and its SQLite WAL/SHM sidecars so the
// restored snapshot isn't paired with a leftover WAL from the DB it
// replaces. Only safe to call while nothing has the DB open (feedla calls
// it between server runs -- see cmd/feedla's restore-and-restart loop).
func PromoteStaged(stagedPath, dbPath string) error {
	if _, err := os.Stat(stagedPath); err != nil {
		return fmt.Errorf("restore: stat staged snapshot %s: %w", stagedPath, err)
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restore: remove %s: %w", p, err)
		}
	}
	if err := os.Rename(stagedPath, dbPath); err != nil {
		return fmt.Errorf("restore: promote staged snapshot: %w", err)
	}
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
