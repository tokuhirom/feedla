// Package maintenance runs feedla's periodic background upkeep: entry
// retention/GC and (per docs/DESIGN.md's "バックアップ" section) daily backups.
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

const defaultInterval = 24 * time.Hour

// tickTimeout bounds a single tick's GC+backup work with its own deadline,
// detached from Run's ctx -- see tick. Generous relative to real workloads
// (VACUUM INTO of a personal-scale DB is seconds, not minutes) while still
// guarding against a genuinely stuck DB call blocking forever.
const tickTimeout = 5 * time.Minute

// orphanFeedGraceDays is the grace period docs/multi-user-design.md's GC
// section specifies before a feed with no subscribers is deleted, so a
// resubscribe within the window can reuse the already-crawled data instead
// of starting cold.
const orphanFeedGraceDays = 7

// Config controls what a Runner does on each tick. RetentionDays <= 0
// disables age-based GC; RetentionPerFeed <= 0 disables the per-feed cap;
// BackupDir == "" disables backups.
type Config struct {
	RetentionDays    int
	RetentionPerFeed int
	BackupDir        string
	Remote           RemoteUploader // nil disables mirroring backups off-host
	Interval         time.Duration  // <= 0 uses defaultInterval (24h)
}

// RemoteUploader mirrors a daily local backup snapshot to off-host storage,
// e.g. *internal/remotebackup.Client. Store uploads the file at localPath
// under key and prunes old generations sharing key's extension.
type RemoteUploader interface {
	Store(ctx context.Context, key, localPath string) error
}

// Runner periodically GCs old read entries per docs/DESIGN.md's "GC / リテンション"
// section (delete read+unpinned entries older than RetentionDays, trim
// read+unpinned entries beyond RetentionPerFeed per feed, then
// PRAGMA optimize) and, per the "バックアップ" section, writes a daily
// VACUUM INTO snapshot plus an OPML export to BackupDir.
type Runner struct {
	st  *store.Store
	cfg Config
}

// NewRunner builds a Runner. cfg.Interval <= 0 falls back to 24h.
func NewRunner(st *store.Store, cfg Config) *Runner {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	return &Runner{st: st, cfg: cfg}
}

// Run blocks, running a maintenance pass every cfg.Interval, until ctx is
// canceled. Before entering that interval loop, it also runs an immediate
// backup if today's local snapshot doesn't exist yet (see
// backupIfMissingToday) -- otherwise, since the ticker's first tick doesn't
// fire until a full Interval after Run starts, a server that's restarted
// daily (or was down across what would've been the previous tick) could
// silently go a long time without a fresh backup. It always returns a
// non-nil error: ctx.Err() on clean shutdown.
func (r *Runner) Run(ctx context.Context) error {
	if r.cfg.BackupDir != "" {
		r.backupIfMissingToday(ctx, time.Now())
	}

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// backupIfMissingToday runs an immediate backup pass if today's local
// snapshot (feedla-YYYYMMDD.db under cfg.BackupDir) doesn't exist yet.
func (r *Runner) backupIfMissingToday(ctx context.Context, now time.Time) {
	stamp := now.Format("20060102")
	dbPath := filepath.Join(r.cfg.BackupDir, "feedla-"+stamp+".db")
	if _, err := os.Stat(dbPath); err == nil {
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Error("maintenance: check today's backup", "error", err)
		return
	}

	backupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tickTimeout)
	defer cancel()

	if ran, err := r.backup(backupCtx, now); err != nil {
		slog.Error("maintenance: startup backup", "error", err)
	} else if ran {
		slog.Info("maintenance: startup backup complete", "dir", r.cfg.BackupDir)
	}
}

// tick runs one GC+backup pass on a context detached from Run's ctx (kept
// alive only up to tickTimeout) rather than the caller's ctx directly --
// otherwise a slow tick (DB contention, a large VACUUM INTO, CI running many
// packages under -race) can have its deadline expire mid-operation purely
// because of when Run's own ctx happens to be canceled, aborting whatever
// store call is in flight and logging a spurious "context deadline
// exceeded" instead of either finishing cleanly or hitting tick's own
// generous budget.
func (r *Runner) tick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tickTimeout)
	defer cancel()
	ctx = tickCtx

	now := time.Now()

	if r.cfg.RetentionDays > 0 {
		before := now.Add(-time.Duration(r.cfg.RetentionDays) * 24 * time.Hour)
		n, err := r.st.DeleteOldReadEntries(ctx, before)
		if err != nil {
			slog.Error("maintenance: delete old read entries", "error", err)
		} else if n > 0 {
			slog.Info("maintenance: deleted old read entries", "count", n)
		}
	}

	if r.cfg.RetentionPerFeed > 0 {
		n, err := r.st.TrimExcessEntries(ctx, r.cfg.RetentionPerFeed)
		if err != nil {
			slog.Error("maintenance: trim excess entries", "error", err)
		} else if n > 0 {
			slog.Info("maintenance: trimmed excess entries", "count", n)
		}
	}

	if n, err := r.st.DeleteOrphanFeeds(ctx, now.Add(-orphanFeedGraceDays*24*time.Hour)); err != nil {
		slog.Error("maintenance: delete orphan feeds", "error", err)
	} else if n > 0 {
		slog.Info("maintenance: deleted orphan feeds", "count", n)
	}

	if n, err := r.st.DeleteExpiredSessions(ctx, now); err != nil {
		slog.Error("maintenance: delete expired sessions", "error", err)
	} else if n > 0 {
		slog.Info("maintenance: deleted expired sessions", "count", n)
	}

	if err := r.st.Optimize(ctx); err != nil {
		slog.Error("maintenance: optimize", "error", err)
	}

	if r.cfg.BackupDir != "" {
		if ran, err := r.backup(ctx, now); err != nil {
			slog.Error("maintenance: backup", "error", err)
		} else if ran {
			slog.Info("maintenance: backup complete", "dir", r.cfg.BackupDir)
		}
	}
}

// backup writes a same-day DB snapshot (VACUUM INTO) and OPML export to
// cfg.BackupDir, named feedla-YYYYMMDD.{db,opml}. Re-running on the same
// day overwrites both files. ran=false, err=nil means the backup was
// deliberately skipped (setup still pending, see below), so callers don't
// log a "backup complete" that never happened.
//
// It refuses to run while initial setup is still pending: a fresh instance
// (or one whose restore-at-boot found nothing and silently started empty)
// would otherwise snapshot an empty DB, upload it under today's key --
// possibly overwriting a good same-day remote backup -- and have the
// remote generation pruning slowly push out every real backup.
func (r *Runner) backup(ctx context.Context, now time.Time) (ran bool, err error) {
	// Same definition of "setup pending" as the API layer: the bootstrap
	// admin (id=1, seeded by migration) still has the sentinel password.
	const bootstrapAdminID = 1
	pending, err := r.st.IsSetupPending(ctx, bootstrapAdminID)
	if err != nil {
		return false, fmt.Errorf("maintenance: backup: check setup state: %w", err)
	}
	if pending {
		slog.Info("maintenance: skipping backup while initial setup is pending")
		return false, nil
	}

	if err := os.MkdirAll(r.cfg.BackupDir, 0o755); err != nil {
		return false, fmt.Errorf("maintenance: backup: mkdir %s: %w", r.cfg.BackupDir, err)
	}

	stamp := now.Format("20060102")

	dbPath := filepath.Join(r.cfg.BackupDir, "feedla-"+stamp+".db")
	if err := r.st.BackupTo(ctx, dbPath); err != nil {
		return false, fmt.Errorf("maintenance: backup: db snapshot: %w", err)
	}

	// Phase B: exactly one user exists (the bootstrap admin, id=1), so the
	// backup's OPML export is admin-wide. Revisit for per-user or
	// admin-selectable export once Phase C adds more users.
	opml, err := feed.ExportOPML(ctx, r.st, bootstrapAdminID)
	if err != nil {
		return false, fmt.Errorf("maintenance: backup: export opml: %w", err)
	}
	opmlPath := filepath.Join(r.cfg.BackupDir, "feedla-"+stamp+".opml")
	if err := os.WriteFile(opmlPath, opml, 0o644); err != nil {
		return false, fmt.Errorf("maintenance: backup: write opml: %w", err)
	}

	if r.cfg.Remote != nil {
		// A remote upload failure (network blip, credential issue) shouldn't
		// be treated as a failed backup -- the local snapshot above already
		// succeeded -- so it's logged rather than returned.
		if err := r.cfg.Remote.Store(ctx, "feedla-"+stamp+".db", dbPath); err != nil {
			slog.Error("maintenance: backup: remote upload db", "error", err)
		}
		if err := r.cfg.Remote.Store(ctx, "feedla-"+stamp+".opml", opmlPath); err != nil {
			slog.Error("maintenance: backup: remote upload opml", "error", err)
		}
	}

	return true, nil
}
