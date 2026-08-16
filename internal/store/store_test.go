package store_test

import (
	"path/filepath"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestOpenAppliesMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedla.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	tables := []string{"feeds", "subscriptions", "folders", "entries", "pins", "entries_fts", "scrape_sources", "schema_migrations"}
	for _, table := range tables {
		var name string
		err := st.Read.QueryRow(`SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing after migrate: %v", table, err)
		}
	}

	// Re-opening (and thus re-migrating) an already-migrated db must be a no-op.
	st2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("second store.Open: %v", err)
	}
	defer st2.Close()
}
