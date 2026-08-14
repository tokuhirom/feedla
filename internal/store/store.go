// Package store provides SQLite-backed persistence for feedla.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by lookups/mutations that target a row which
// doesn't exist, so callers (typically the API layer) can map it to a 404
// without string-matching error messages.
var ErrNotFound = errors.New("store: not found")

// Store holds the two connection pools required to use SQLite safely from a
// concurrent Go process: a read pool with several connections, and a write
// pool limited to a single connection so writes are serialized by the
// application instead of colliding on SQLITE_BUSY.
type Store struct {
	Read  *sql.DB
	Write *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path, applies
// pending migrations, and returns a ready-to-use Store. Callers must call
// Close when done.
func Open(path string) (*Store, error) {
	writeDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open write db: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	if err := applyPragmas(writeDB); err != nil {
		writeDB.Close()
		return nil, err
	}

	if err := migrate(writeDB); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	readDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("store: open read db: %w", err)
	}
	readDB.SetMaxOpenConns(max(1, runtime.NumCPU()))
	if err := applyPragmas(readDB); err != nil {
		writeDB.Close()
		readDB.Close()
		return nil, err
	}

	return &Store{Read: readDB, Write: writeDB}, nil
}

// Close releases both connection pools.
func (s *Store) Close() error {
	writeErr := s.Write.Close()
	readErr := s.Read.Close()
	if writeErr != nil {
		return writeErr
	}
	return readErr
}

func dsn(path string) string {
	// busy_timeout and temp_store are also set via PRAGMA below, but setting
	// them in the DSN makes the driver reapply them to every new physical
	// connection it opens (not just the one applyPragmas happened to run
	// against right after sql.Open) -- readDB's pool grows past one
	// connection under concurrent load, so this is the only way to
	// guarantee later connections get them too.
	return path + "?_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)"
}

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous  = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA cache_size   = -20000",
		// The scratch-based Docker image (see Dockerfile) has no /tmp, so
		// SQLite's disk-backed temp store fails with "disk I/O error"
		// (SQLITE_IOERR_GETTEMPPATH) as soon as a query needs a temp
		// b-tree/file (e.g. MarkEntriesRead's bulk UPDATE ... WHERE id IN
		// (...)). Keeping TEMP tables/indices in memory avoids touching the
		// filesystem for them entirely.
		"PRAGMA temp_store   = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("store: %s: %w", p, err)
		}
	}
	return nil
}
