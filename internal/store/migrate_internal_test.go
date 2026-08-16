package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// newRawMigrationTestDB opens a bare DB with the same connection setup
// store.Open uses for its Write pool (single connection, pragmas applied),
// but without running the embedded migrations -- so tests can feed
// applyMigration hand-written scripts directly.
func newRawMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := applyPragmas(db); err != nil {
		t.Fatalf("applyPragmas: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL DEFAULT (unixepoch())
		)
	`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	return db
}

// TestApplyMigrationSurvivesParentTableRebuild reproduces the bug this
// package's applyMigration works around: rebuilding a parent table
// (create-new -> copy -> drop-old -> rename, SQLite's only way to change a
// primary key) while foreign_keys enforcement is on fires the child's
// ON DELETE clause against the dropped parent, corrupting the child's rows
// before the new parent table is even in place. See Phase B's migration
// 0006, which rebuilds subscriptions/folders/pins/ignore_words this way.
func TestApplyMigrationSurvivesParentTableRebuild(t *testing.T) {
	db := newRawMigrationTestDB(t)

	initial := `
		CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id) ON DELETE SET NULL);
		INSERT INTO parent (id, name) VALUES (1, 'p1');
		INSERT INTO child (id, parent_id) VALUES (1, 1);
	`
	if err := applyMigration(db, "0001_init.sql", initial); err != nil {
		t.Fatalf("apply initial: %v", err)
	}

	rebuild := `
		CREATE TABLE parent_new (id INTEGER PRIMARY KEY, name TEXT, extra TEXT NOT NULL DEFAULT '');
		INSERT INTO parent_new (id, name) SELECT id, name FROM parent;
		DROP TABLE parent;
		ALTER TABLE parent_new RENAME TO parent;
	`
	if err := applyMigration(db, "0002_rebuild.sql", rebuild); err != nil {
		t.Fatalf("apply rebuild: %v", err)
	}

	var parentID sql.NullInt64
	if err := db.QueryRow(`SELECT parent_id FROM child WHERE id = 1`).Scan(&parentID); err != nil {
		t.Fatalf("query child: %v", err)
	}
	if !parentID.Valid || parentID.Int64 != 1 {
		t.Fatalf("child.parent_id = %v, want 1 (FK cascade must not fire while rebuilding the referenced parent table)", parentID)
	}
}

// TestApplyMigrationRejectsForeignKeyViolation confirms the
// PRAGMA foreign_key_check gate still catches a script that leaves the
// database in a genuinely inconsistent state -- disabling enforcement for
// the rebuild trick must not also silently accept unrelated FK breakage.
func TestApplyMigrationRejectsForeignKeyViolation(t *testing.T) {
	db := newRawMigrationTestDB(t)

	initial := `
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id));
	`
	if err := applyMigration(db, "0001_init.sql", initial); err != nil {
		t.Fatalf("apply initial: %v", err)
	}

	broken := `INSERT INTO child (id, parent_id) VALUES (1, 999);` // 999 doesn't exist in parent
	if err := applyMigration(db, "0002_broken.sql", broken); err == nil {
		t.Fatal("apply broken: want an error from foreign_key_check, got nil")
	}

	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, "0002_broken.sql").Scan(&applied); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if applied != 0 {
		t.Fatal("0002_broken.sql was recorded as applied despite the foreign_key_check failure (rollback didn't happen)")
	}

	var childCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM child`).Scan(&childCount); err != nil {
		t.Fatalf("query child: %v", err)
	}
	if childCount != 0 {
		t.Fatalf("child has %d rows, want 0 (the bad insert should have been rolled back)", childCount)
	}
}
