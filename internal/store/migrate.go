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

func applyMigration(db *sql.DB, name, script string) error {
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
	return tx.Commit()
}
