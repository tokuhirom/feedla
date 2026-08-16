package store

import (
	"database/sql"
	"fmt"
	"sort"
)

// migrate applies every embedded *.sql file under migrations/ that hasn't
// been applied yet, in filename order, each inside its own transaction.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL DEFAULT (unixepoch())
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := applyMigration(db, name, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func isApplied(db *sql.DB, name string) (bool, error) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE name = ?`, name).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return true, nil
}

// applyMigration runs script inside its own transaction. foreign_keys is
// toggled off around the whole thing: PRAGMA foreign_keys has no effect
// inside a transaction (SQLite only honors it between transactions), so a
// migration that rebuilds a table other tables reference via FK (create new
// -> copy -> drop old -> rename, the only way SQLite can change a primary
// key) would otherwise have DROP TABLE fire ON DELETE SET NULL/CASCADE
// against every referencing row while enforcement is still on -- verified by
// reproducing it: rebuilding a parent table while a child's FK is live
// NULLs/deletes the child's rows for real, even before the new table is
// renamed into place. Store.Open's Write pool is capped at one connection
// (see store.go) and migrate runs before Read is even opened, so this
// toggle, the transaction, and the later re-enable are guaranteed to run on
// the same physical connection -- required for a per-connection PRAGMA to
// mean anything here.
func applyMigration(db *sql.DB, name, script string) error {
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}
	// Best-effort re-enable on any early return (error paths); the success
	// path below re-enables explicitly so it can report a failure there.
	defer func() { _, _ = db.Exec(`PRAGMA foreign_keys = ON`) }()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(script); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
		return err
	}

	// Catch any FK inconsistency the script introduced while enforcement
	// was off, before committing it permanently.
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	hasViolation := rows.Next()
	if cerr := rows.Close(); cerr != nil {
		return fmt.Errorf("foreign_key_check: %w", cerr)
	}
	if hasViolation {
		return fmt.Errorf("foreign_key_check found violations after applying %s", name)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("re-enable foreign_keys: %w", err)
	}
	return nil
}
