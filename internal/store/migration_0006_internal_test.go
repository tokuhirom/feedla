package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestMigration0006PreservesPreExistingData seeds a DB with only migrations
// 0001-0005 applied (i.e. the pre-Phase-B, single-user schema: entries.
// read_at/ignored, subscriptions keyed by feed_id alone, no
// user_entry_state), populates it the way a real long-running instance
// would have data, then applies 0006 and checks nothing was silently lost
// or corrupted -- specifically the FK-cascade-during-table-rebuild bug this
// package's applyMigration works around (see migrate_internal_test.go),
// exercised here against the real 0006 script rather than a synthetic one.
func TestMigration0006PreservesPreExistingData(t *testing.T) {
	db := newRawMigrationTestDB(t)
	ctx := context.Background()

	names, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, e := range names {
		// Only the pre-Phase-B migrations (0001-0005) run here; 0006 is
		// applied explicitly below, and anything after it (e.g. 0010,
		// which depends on user_entry_state) must not run before 0006.
		if e.Name() >= "0006_multi_user_data.sql" {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := applyMigration(db, e.Name(), string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", e.Name(), err)
		}
	}

	// Seed pre-Phase-B data directly against the old (0001-0005) schema:
	// a folder, a subscription filed under it, an ignore word, and three
	// entries (read+unfiltered, unread, ignored-by-word) plus a pin.
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO folders (id, name, sort_order) VALUES (1, 'Tech', 0)`); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO feeds (id, feed_url, title, fetch_interval_sec, next_fetch_at, created_at, updated_at)
		VALUES (1, 'https://example.com/feed', 'Example', 1800, ?, ?, ?)
	`, now, now, now); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (feed_id, folder_id, title, rating, unread_count, sort_order, created_at)
		VALUES (1, 1, 'Example', 3, 1, 0, ?)
	`, now); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ignore_words (id, word, created_at) VALUES (1, 'spoiler', ?)`, now); err != nil {
		t.Fatalf("seed ignore word: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO entries (id, feed_id, guid, url, title, body, body_hash, published_at, updated_at, fetched_at, read_at, ignored)
		VALUES
			(1, 1, 'read',    'https://example.com/1', 'Read entry',    'body', x'00', ?, ?, ?, ?, 0),
			(2, 1, 'unread',  'https://example.com/2', 'Unread entry',  'body', x'01', ?, ?, ?, NULL, 0),
			(3, 1, 'ignored', 'https://example.com/3', 'A spoiler here', 'body', x'02', ?, ?, ?, NULL, 1)
	`, now, now, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("seed entries: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pins (entry_id, url, title, created_at) VALUES (2, 'https://example.com/2', 'Unread entry', ?)`, now); err != nil {
		t.Fatalf("seed pin: %v", err)
	}

	sqlBytes, err := migrationsFS.ReadFile("migrations/0006_multi_user_data.sql")
	if err != nil {
		t.Fatalf("read 0006: %v", err)
	}
	if err := applyMigration(db, "0006_multi_user_data.sql", string(sqlBytes)); err != nil {
		t.Fatalf("apply 0006: %v", err)
	}

	// FK integrity: the whole point of the OFF/check/ON dance in
	// applyMigration.
	fkRows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	if fkRows.Next() {
		t.Error("foreign_key_check found violations after 0006")
	}
	_ = fkRows.Close()

	// folder_id must have survived the folders table rebuild (this is
	// exactly what the FK-cascade bug would have NULLed out).
	var folderID sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT folder_id FROM subscriptions WHERE user_id = 1 AND feed_id = 1`).Scan(&folderID); err != nil {
		t.Fatalf("query subscription folder_id: %v", err)
	}
	if !folderID.Valid || folderID.Int64 != 1 {
		t.Fatalf("subscriptions.folder_id = %v, want 1 (must survive the folders table rebuild)", folderID)
	}

	var folderName string
	if err := db.QueryRowContext(ctx, `SELECT name FROM folders WHERE id = 1 AND user_id = 1`).Scan(&folderName); err != nil {
		t.Fatalf("query folder: %v", err)
	}
	if folderName != "Tech" {
		t.Fatalf("folder name = %q, want Tech", folderName)
	}

	// unread_count must match a fresh count from user_entry_state.
	var unreadCount, recomputed int64
	if err := db.QueryRowContext(ctx, `SELECT unread_count FROM subscriptions WHERE user_id = 1 AND feed_id = 1`).Scan(&unreadCount); err != nil {
		t.Fatalf("query unread_count: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_entry_state WHERE user_id = 1 AND feed_id = 1 AND read_at IS NULL AND ignored = 0
	`).Scan(&recomputed); err != nil {
		t.Fatalf("recompute unread_count: %v", err)
	}
	if unreadCount != recomputed || unreadCount != 1 {
		t.Fatalf("unread_count = %d, recomputed = %d, want both 1 (only 'unread' qualifies: 'read' is read, 'ignored' is ignored)", unreadCount, recomputed)
	}

	// Backfilled read/ignored state, per entry, in user_entry_state.
	wantReadAtNil := map[string]bool{"read": false, "unread": true, "ignored": true}
	wantIgnored := map[string]bool{"read": false, "unread": false, "ignored": true}
	rows, err := db.QueryContext(ctx, `
		SELECT e.guid, ues.read_at, ues.ignored
		FROM entries e
		JOIN user_entry_state ues ON ues.entry_id = e.id AND ues.user_id = 1
	`)
	if err != nil {
		t.Fatalf("query user_entry_state: %v", err)
	}
	seen := map[string]bool{}
	for rows.Next() {
		var guid string
		var readAt sql.NullInt64
		var ignored bool
		if err := rows.Scan(&guid, &readAt, &ignored); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen[guid] = true
		if (!readAt.Valid) != wantReadAtNil[guid] {
			t.Errorf("guid %q: read_at valid = %v, want NULL=%v", guid, readAt.Valid, wantReadAtNil[guid])
		}
		if ignored != wantIgnored[guid] {
			t.Errorf("guid %q: ignored = %v, want %v", guid, ignored, wantIgnored[guid])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	_ = rows.Close()
	for _, guid := range []string{"read", "unread", "ignored"} {
		if !seen[guid] {
			t.Errorf("guid %q missing from user_entry_state after backfill", guid)
		}
	}

	// entries.read_at/ignored columns themselves must be gone.
	if _, err := db.ExecContext(ctx, `SELECT read_at FROM entries LIMIT 1`); err == nil {
		t.Error("entries.read_at still exists after 0006, want it dropped")
	}
	if _, err := db.ExecContext(ctx, `SELECT ignored FROM entries LIMIT 1`); err == nil {
		t.Error("entries.ignored still exists after 0006, want it dropped")
	}

	// pins must have survived, now keyed by (user_id, entry_id).
	var pinUserID int64
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM pins WHERE entry_id = 2`).Scan(&pinUserID); err != nil {
		t.Fatalf("query pin: %v", err)
	}
	if pinUserID != 1 {
		t.Fatalf("pin user_id = %d, want 1", pinUserID)
	}

	// ignore_words must have survived, now user-scoped.
	var wordUserID int64
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM ignore_words WHERE word = 'spoiler'`).Scan(&wordUserID); err != nil {
		t.Fatalf("query ignore word: %v", err)
	}
	if wordUserID != 1 {
		t.Fatalf("ignore_words.user_id = %d, want 1", wordUserID)
	}

	// scrape_sources.created_by must default to the bootstrap admin (no
	// scrape_sources rows exist in this seed, but the column itself must be
	// usable -- confirm via a fresh insert relying on the DEFAULT).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scrape_sources (feed_id, kind, target_url, created_at, updated_at) VALUES (1, 'pagewatch', 'https://example.com/', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert scrape_source relying on created_by default: %v", err)
	}
	var createdBy int64
	if err := db.QueryRowContext(ctx, `SELECT created_by FROM scrape_sources WHERE feed_id = 1`).Scan(&createdBy); err != nil {
		t.Fatalf("query scrape_source created_by: %v", err)
	}
	if createdBy != 1 {
		t.Fatalf("scrape_sources.created_by = %d, want 1 (DEFAULT)", createdBy)
	}
}
