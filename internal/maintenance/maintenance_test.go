package maintenance_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/maintenance"
	"github.com/tokuhirom/feedla/internal/store"
)

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
