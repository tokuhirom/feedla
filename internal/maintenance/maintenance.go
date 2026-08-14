// Package maintenance runs feedla's periodic background upkeep: entry
// retention/GC and (per README's "バックアップ" section) daily backups.
package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

const defaultInterval = 24 * time.Hour

// Config controls what a Runner does on each tick. RetentionDays <= 0
// disables age-based GC; RetentionPerFeed <= 0 disables the per-feed cap.
type Config struct {
	RetentionDays    int
	RetentionPerFeed int
	Interval         time.Duration // <= 0 uses defaultInterval (24h)
}

// Runner periodically GCs old read entries per README's "GC / リテンション"
// section: delete read+unpinned entries older than RetentionDays, trim
// read+unpinned entries beyond RetentionPerFeed per feed, then
// PRAGMA optimize.
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
// canceled. It always returns a non-nil error: ctx.Err() on clean shutdown.
func (r *Runner) Run(ctx context.Context) error {
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

func (r *Runner) tick(ctx context.Context) {
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

	if err := r.st.Optimize(ctx); err != nil {
		slog.Error("maintenance: optimize", "error", err)
	}
}
