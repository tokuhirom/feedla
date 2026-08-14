package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UpsertEntries writes a feed's parsed entries inside one transaction and
// returns how many were brand new (as opposed to updates to an
// already-known guid). New entries are inserted with read_at = NULL;
// existing entries have everything except published_at/read_at refreshed,
// so re-fetching never un-reads an entry or moves its position.
func (s *Store) UpsertEntries(ctx context.Context, feedID int64, entries []EntryInput, now time.Time) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: begin tx: %w", err)
	}
	defer tx.Rollback()

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries(feed_id, guid, url, title, author, body, body_hash, published_at, updated_at, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_id, guid) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: prepare insert: %w", err)
	}
	defer func() { _ = insertStmt.Close() }()

	updateStmt, err := tx.PrepareContext(ctx, `
		UPDATE entries SET
			url = ?, title = ?, author = ?, body = ?, body_hash = ?, updated_at = ?, fetched_at = ?
		WHERE feed_id = ? AND guid = ?
	`)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: prepare update: %w", err)
	}
	defer func() { _ = updateStmt.Close() }()

	newCount := 0
	for _, e := range entries {
		res, err := insertStmt.ExecContext(ctx, feedID, e.GUID, e.URL, e.Title, e.Author, e.Body, e.BodyHash, e.PublishedAt, e.UpdatedAt, now.Unix())
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: insert: %w", e.GUID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: rows affected: %w", e.GUID, err)
		}
		if n > 0 {
			newCount++
			continue
		}

		if _, err := updateStmt.ExecContext(ctx, e.URL, e.Title, e.Author, e.Body, e.BodyHash, e.UpdatedAt, now.Unix(), feedID, e.GUID); err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: update: %w", e.GUID, err)
		}
	}

	if newCount > 0 {
		if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: upsert entries: commit: %w", err)
	}
	return newCount, nil
}

func refreshUnreadCount(ctx context.Context, tx *sql.Tx, feedID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET unread_count = (
			SELECT COUNT(*) FROM entries WHERE feed_id = ? AND read_at IS NULL
		) WHERE feed_id = ?
	`, feedID, feedID); err != nil {
		return fmt.Errorf("store: refresh unread_count for feed %d: %w", feedID, err)
	}
	return nil
}

// CountEntries returns how many entries exist for feedID. Test helper.
func (s *Store) CountEntries(ctx context.Context, feedID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE feed_id = ?`, feedID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count entries: %w", err)
	}
	return n, nil
}

// ListEntries returns up to limit entries for feedID, newest first (matching
// idx_entries_feed_pub's (published_at DESC, id DESC) order). unreadOnly
// restricts to unread entries. cursor, if non-nil, resumes after the given
// (published_at, id) — pass the last entry of the previous page.
func (s *Store) ListEntries(ctx context.Context, feedID int64, unreadOnly bool, limit int, cursor *EntryCursor) ([]Entry, error) {
	query := `
		SELECT e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.body, e.published_at, e.updated_at, e.fetched_at, e.read_at,
			p.entry_id IS NOT NULL
		FROM entries e
		LEFT JOIN pins p ON p.entry_id = e.id
		WHERE e.feed_id = ?
	`
	args := []any{feedID}
	if unreadOnly {
		query += ` AND e.read_at IS NULL`
	}
	if cursor != nil {
		query += ` AND (e.published_at < ? OR (e.published_at = ? AND e.id < ?))`
		args = append(args, cursor.PublishedAt, cursor.PublishedAt, cursor.ID)
	}
	query += ` ORDER BY e.published_at DESC, e.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list entries for feed %d: %w", feedID, err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.GUID, &e.URL, &e.Title, &e.Author, &e.Body, &e.PublishedAt, &e.UpdatedAt, &e.FetchedAt, &e.ReadAt, &e.Pinned); err != nil {
			return nil, fmt.Errorf("store: scan entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SearchEntries full-text searches title/body across every feed, newest
// first, paginated the same way as ListEntries. Queries shorter than 3
// characters fall back to a LIKE scan since FTS5 trigram tokenization
// generally can't match them; personal-scale entry counts make the full
// table scan acceptable.
func (s *Store) SearchEntries(ctx context.Context, query string, limit int, cursor *EntryCursor) ([]Entry, error) {
	var rows *sql.Rows
	var err error

	base := `
		SELECT e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.body, e.published_at, e.updated_at, e.fetched_at, e.read_at,
			p.entry_id IS NOT NULL
		FROM %s
		LEFT JOIN pins p ON p.entry_id = e.id
		WHERE %s
	`

	if len([]rune(query)) < 3 {
		sqlQuery := fmt.Sprintf(base, "entries e", "(e.title LIKE ? OR e.body LIKE ?)")
		like := "%" + query + "%"
		args := []any{like, like}
		if cursor != nil {
			sqlQuery += ` AND (e.published_at < ? OR (e.published_at = ? AND e.id < ?))`
			args = append(args, cursor.PublishedAt, cursor.PublishedAt, cursor.ID)
		}
		sqlQuery += ` ORDER BY e.published_at DESC, e.id DESC LIMIT ?`
		args = append(args, limit)
		rows, err = s.Read.QueryContext(ctx, sqlQuery, args...)
	} else {
		sqlQuery := fmt.Sprintf(base, "entries_fts JOIN entries e ON e.id = entries_fts.rowid", "entries_fts MATCH ?")
		phrase := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		args := []any{phrase}
		if cursor != nil {
			sqlQuery += ` AND (e.published_at < ? OR (e.published_at = ? AND e.id < ?))`
			args = append(args, cursor.PublishedAt, cursor.PublishedAt, cursor.ID)
		}
		sqlQuery += ` ORDER BY e.published_at DESC, e.id DESC LIMIT ?`
		args = append(args, limit)
		rows, err = s.Read.QueryContext(ctx, sqlQuery, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("store: search entries %q: %w", query, err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.GUID, &e.URL, &e.Title, &e.Author, &e.Body, &e.PublishedAt, &e.UpdatedAt, &e.FetchedAt, &e.ReadAt, &e.Pinned); err != nil {
			return nil, fmt.Errorf("store: scan entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// MarkEntriesRead sets read_at = now for every given entry id that's
// currently unread, and refreshes unread_count for every feed touched. It
// returns how many entries were actually marked (already-read ids don't
// count).
func (s *Store) MarkEntriesRead(ctx context.Context, entryIDs []int64, now time.Time) (int, error) {
	if len(entryIDs) == 0 {
		return 0, nil
	}

	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: mark entries read: begin tx: %w", err)
	}
	defer tx.Rollback()

	placeholders := make([]string, len(entryIDs))
	idArgs := make([]any, len(entryIDs))
	for i, id := range entryIDs {
		placeholders[i] = "?"
		idArgs[i] = id
	}
	inClause := "id IN (" + strings.Join(placeholders, ",") + ")"

	feedIDs, err := queryInt64s(ctx, tx,
		`SELECT DISTINCT feed_id FROM entries WHERE `+inClause+` AND read_at IS NULL`, idArgs...)
	if err != nil {
		return 0, fmt.Errorf("store: mark entries read: find feeds: %w", err)
	}

	updateArgs := append([]any{now.Unix()}, idArgs...)
	res, err := tx.ExecContext(ctx,
		`UPDATE entries SET read_at = ? WHERE `+inClause+` AND read_at IS NULL`, updateArgs...)
	if err != nil {
		return 0, fmt.Errorf("store: mark entries read: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: mark entries read: rows affected: %w", err)
	}

	for _, feedID := range feedIDs {
		if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: mark entries read: commit: %w", err)
	}
	return int(affected), nil
}

func queryInt64s(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// MarkFeedReadBefore marks every unread entry of feedID read (LDR's
// touch_all). If before > 0, only entries published at or before that unix
// timestamp are touched; otherwise every unread entry is. It returns how
// many entries were marked.
func (s *Store) MarkFeedReadBefore(ctx context.Context, feedID int64, before int64, now time.Time) (int, error) {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: mark feed read: begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE entries SET read_at = ?
		WHERE feed_id = ? AND read_at IS NULL AND (? <= 0 OR published_at <= ?)
	`, now.Unix(), feedID, before, before)
	if err != nil {
		return 0, fmt.Errorf("store: mark feed %d read: %w", feedID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: mark feed %d read: rows affected: %w", feedID, err)
	}

	if affected > 0 {
		if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: mark feed %d read: commit: %w", feedID, err)
	}
	return int(affected), nil
}
