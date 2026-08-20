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
// already-known guid). New entries are fanned out into user_entry_state for
// every current subscriber of feedID (docs/multi-user-design.md's
// fan-out-on-write design; read_at = NULL, ignored computed per subscriber
// against their own ignore_words), in the same transaction as the insert.
// entries.read_at/ignored no longer exist (moved to user_entry_state by
// migration 0006); the crawler has no per-user context, so this function's
// signature stays feed-scoped.
//
// A feed that carries no dates at all (EntryInput.DateMissing) has every
// item stamped with the same crawl-time PublishedAt, so a single crawl can
// otherwise dump an entire backlog in as unread, all sorted as "latest" --
// see issue #75. Guard against that by inserting only the first
// DateMissing entry in feed order (its topmost/newest by the feed's own
// ordering) as unread; the rest of that batch's DateMissing entries are
// inserted already read (in every subscriber's fan-out row too).
func (s *Store) UpsertEntries(ctx context.Context, feedID int64, entries []EntryInput, now time.Time) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	firstDateMissingGUID := ""
	for _, e := range entries {
		if e.DateMissing {
			firstDateMissingGUID = e.GUID
			break
		}
	}

	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: begin tx: %w", err)
	}
	defer tx.Rollback()

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries(feed_id, guid, url, title, author, body, body_hash, published_at, updated_at, fetched_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

	fanOutStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, created_at, read_at, ignored)
		SELECT s.user_id, ?, ?, ?, ?, ?, EXISTS(
			SELECT 1 FROM ignore_words iw
			WHERE iw.user_id = s.user_id AND (? LIKE '%' || iw.word || '%' OR ? LIKE '%' || iw.word || '%')
		)
		FROM subscriptions s WHERE s.feed_id = ?
	`)
	if err != nil {
		return 0, fmt.Errorf("store: upsert entries: prepare fan out: %w", err)
	}
	defer func() { _ = fanOutStmt.Close() }()

	newCount := 0
	for _, e := range entries {
		var readAt sql.NullInt64
		if e.DateMissing && e.GUID != firstDateMissingGUID {
			readAt = sql.NullInt64{Int64: now.Unix(), Valid: true}
		}
		res, err := insertStmt.ExecContext(ctx, feedID, e.GUID, e.URL, e.Title, e.Author, e.Body, e.BodyHash, e.PublishedAt, e.UpdatedAt, now.Unix(), now.Unix())
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: insert: %w", e.GUID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: upsert entry %q: rows affected: %w", e.GUID, err)
		}
		if n > 0 {
			newCount++
			entryID, err := res.LastInsertId()
			if err != nil {
				return 0, fmt.Errorf("store: upsert entry %q: last insert id: %w", e.GUID, err)
			}
			if _, err := fanOutStmt.ExecContext(ctx, entryID, feedID, e.PublishedAt, now.Unix(), readAt, e.Title, e.Body, feedID); err != nil {
				return 0, fmt.Errorf("store: upsert entry %q: fan out: %w", e.GUID, err)
			}
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

// refreshUnreadCount recomputes unread_count for every subscriber of
// feedID from user_entry_state, the source of truth.
func refreshUnreadCount(ctx context.Context, tx *sql.Tx, feedID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET unread_count = (
			SELECT COUNT(*) FROM user_entry_state ues
			WHERE ues.user_id = subscriptions.user_id AND ues.feed_id = ?
			  AND ues.read_at IS NULL AND ues.ignored = 0
		) WHERE feed_id = ?
	`, feedID, feedID); err != nil {
		return fmt.Errorf("store: refresh unread_count for feed %d: %w", feedID, err)
	}
	return nil
}

// ExistingEntryGUIDs returns which of the given guids already have an
// entries row for feedID. Callers use this to tell genuinely-new entries
// apart from ones UpsertEntries would just update -- e.g. the fulltext
// crawler integration (internal/crawler/fulltext.go) only fetches an
// entry's own link for guids not yet in the store, so re-crawling a feed
// never re-fetches every article page it has already extracted.
func (s *Store) ExistingEntryGUIDs(ctx context.Context, feedID int64, guids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(guids))
	if len(guids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(guids))
	args := make([]any, 0, len(guids)+1)
	args = append(args, feedID)
	for i, g := range guids {
		placeholders[i] = "?"
		args = append(args, g)
	}

	rows, err := s.Read.QueryContext(ctx,
		`SELECT guid FROM entries WHERE feed_id = ? AND guid IN (`+strings.Join(placeholders, ",")+`)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("store: existing entry guids for feed %d: %w", feedID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("store: scan existing entry guid: %w", err)
		}
		out[g] = true
	}
	return out, rows.Err()
}

// CountEntries returns how many entries exist for feedID. Test helper.
func (s *Store) CountEntries(ctx context.Context, feedID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE feed_id = ?`, feedID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count entries: %w", err)
	}
	return n, nil
}

// ListEntries returns up to limit entries for feedID that userID
// subscribes to, newest first (matching idx_entries_feed_pub's
// (published_at DESC, id DESC) order). unreadOnly restricts to unread
// entries. cursor, if non-nil, resumes after the given (published_at, id)
// — pass the last entry of the previous page.
func (s *Store) ListEntries(ctx context.Context, userID, feedID int64, unreadOnly bool, limit int, cursor *EntryCursor) ([]Entry, error) {
	query := `
		SELECT e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.body, e.published_at, e.updated_at, e.fetched_at, ues.read_at,
			p.entry_id IS NOT NULL
		FROM entries e
		JOIN user_entry_state ues ON ues.entry_id = e.id AND ues.user_id = ?
		LEFT JOIN pins p ON p.entry_id = e.id AND p.user_id = ?
		WHERE e.feed_id = ? AND ues.ignored = 0
	`
	args := []any{userID, userID, feedID}
	if unreadOnly {
		query += ` AND ues.read_at IS NULL`
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

// ListEntriesByFolder lists entries across every subscription userID has
// filed under folderID (nil means the unfiled bucket), newest first,
// paginated the same way as ListEntries. This backs the sidebar's "read
// everything in this folder at once" view.
func (s *Store) ListEntriesByFolder(ctx context.Context, userID int64, folderID *int64, unreadOnly bool, limit int, cursor *EntryCursor) ([]Entry, error) {
	if folderID == nil {
		return s.listGroupEntries(ctx, userID, "s.folder_id IS NULL", nil, unreadOnly, limit, cursor)
	}
	return s.listGroupEntries(ctx, userID, "s.folder_id = ?", []any{*folderID}, unreadOnly, limit, cursor)
}

// ListEntriesByRating lists entries across every subscription userID rated
// exactly rating (0-5), newest first, paginated the same way as
// ListEntries. This backs the sidebar's priority-mode "read everything at
// this ★ level" view.
func (s *Store) ListEntriesByRating(ctx context.Context, userID, rating int64, unreadOnly bool, limit int, cursor *EntryCursor) ([]Entry, error) {
	return s.listGroupEntries(ctx, userID, "s.rating = ?", []any{rating}, unreadOnly, limit, cursor)
}

// ListTodayEntries lists every unread entry userID has that was newly
// registered (entries.created_at, set only on first INSERT -- not
// entries.published_at, the feed-supplied date, which can be far in the
// past for a backlog a feed only just started serving) at or after
// sinceUnix across every feed they subscribe to, regardless of rating --
// the sidebar's pinned "Today" group, newest first, paginated the same way
// as ListEntries. Always unread-only by definition (no unreadOnly toggle,
// unlike ListEntriesByFolder/ListEntriesByRating).
func (s *Store) ListTodayEntries(ctx context.Context, userID, sinceUnix int64, limit int, cursor *EntryCursor) ([]Entry, error) {
	return s.listGroupEntries(ctx, userID, "e.created_at >= ?", []any{sinceUnix}, true, limit, cursor)
}

// CountTodayUnread returns how many of userID's unread, non-ignored entries
// were newly registered at or after sinceUnix -- backs the sidebar's Today
// badge. Matches ListTodayEntries's filter minus pagination. created_at is
// denormalized onto user_entry_state, so this needs no join to entries.
func (s *Store) CountTodayUnread(ctx context.Context, userID, sinceUnix int64) (int64, error) {
	var n int64
	if err := s.Read.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_entry_state
		WHERE user_id = ? AND ignored = 0 AND read_at IS NULL AND created_at >= ?
	`, userID, sinceUnix).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count today unread: %w", err)
	}
	return n, nil
}

func (s *Store) listGroupEntries(ctx context.Context, userID int64, subWhere string, subArgs []any, unreadOnly bool, limit int, cursor *EntryCursor) ([]Entry, error) {
	query := `
		SELECT e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.body, e.published_at, e.updated_at, e.fetched_at, ues.read_at,
			p.entry_id IS NOT NULL
		FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = ?
		JOIN user_entry_state ues ON ues.entry_id = e.id AND ues.user_id = ?
		LEFT JOIN pins p ON p.entry_id = e.id AND p.user_id = ?
		WHERE ues.ignored = 0 AND ` + subWhere + `
	`
	args := append([]any{userID, userID, userID}, subArgs...)
	if unreadOnly {
		query += ` AND ues.read_at IS NULL`
	}
	if cursor != nil {
		query += ` AND (e.published_at < ? OR (e.published_at = ? AND e.id < ?))`
		args = append(args, cursor.PublishedAt, cursor.PublishedAt, cursor.ID)
	}
	query += ` ORDER BY e.published_at DESC, e.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list group entries: %w", err)
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

// SearchEntries full-text searches title/body across every feed userID
// subscribes to, newest first, paginated the same way as ListEntries.
// Queries shorter than 3 characters fall back to a LIKE scan since FTS5
// trigram tokenization generally can't match them; personal-scale entry
// counts make the full table scan acceptable.
func (s *Store) SearchEntries(ctx context.Context, userID int64, query string, limit int, cursor *EntryCursor) ([]Entry, error) {
	var rows *sql.Rows
	var err error

	base := `
		SELECT e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.body, e.published_at, e.updated_at, e.fetched_at, ues.read_at,
			p.entry_id IS NOT NULL
		FROM %s
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = ?
		JOIN user_entry_state ues ON ues.entry_id = e.id AND ues.user_id = ?
		LEFT JOIN pins p ON p.entry_id = e.id AND p.user_id = ?
		WHERE ues.ignored = 0 AND %s
	`

	if len([]rune(query)) < 3 {
		sqlQuery := fmt.Sprintf(base, "entries e", "(e.title LIKE ? OR e.body LIKE ?)")
		like := "%" + query + "%"
		args := []any{userID, userID, userID, like, like}
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
		args := []any{userID, userID, userID, phrase}
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

// MarkEntriesRead sets read_at = now for every given entry id userID has
// unread, and refreshes unread_count for every feed touched. It returns how
// many entries were actually marked. entry ids userID has no
// user_entry_state row for (not subscribed, or doesn't exist) are silently
// skipped rather than erroring -- the bulk-operation authorization rule
// from docs/multi-user-design.md (don't turn a mixed valid/invalid id list
// into an oracle for which ids exist).
func (s *Store) MarkEntriesRead(ctx context.Context, userID int64, entryIDs []int64, now time.Time) (int, error) {
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
	inClause := "entry_id IN (" + strings.Join(placeholders, ",") + ")"

	feedIDs, err := queryInt64s(ctx, tx,
		`SELECT DISTINCT feed_id FROM user_entry_state WHERE user_id = ? AND `+inClause+` AND read_at IS NULL`,
		append([]any{userID}, idArgs...)...)
	if err != nil {
		return 0, fmt.Errorf("store: mark entries read: find feeds: %w", err)
	}

	updateArgs := append([]any{now.Unix(), userID}, idArgs...)
	res, err := tx.ExecContext(ctx,
		`UPDATE user_entry_state SET read_at = ? WHERE user_id = ? AND `+inClause+` AND read_at IS NULL`, updateArgs...)
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

// MarkAllEntriesRead sets read_at = now for every unread entry userID has
// across every feed, and refreshes unread_count for every feed touched. It
// returns how many entries were marked.
func (s *Store) MarkAllEntriesRead(ctx context.Context, userID int64, now time.Time) (int, error) {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: mark all entries read: begin tx: %w", err)
	}
	defer tx.Rollback()

	feedIDs, err := queryInt64s(ctx, tx, `SELECT DISTINCT feed_id FROM user_entry_state WHERE user_id = ? AND read_at IS NULL`, userID)
	if err != nil {
		return 0, fmt.Errorf("store: mark all entries read: find feeds: %w", err)
	}

	res, err := tx.ExecContext(ctx, `UPDATE user_entry_state SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, now.Unix(), userID)
	if err != nil {
		return 0, fmt.Errorf("store: mark all entries read: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: mark all entries read: rows affected: %w", err)
	}

	for _, feedID := range feedIDs {
		if err := refreshUnreadCount(ctx, tx, feedID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: mark all entries read: commit: %w", err)
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

// MarkFeedReadBefore marks every unread entry of feedID userID has read
// (LDR's touch_all). If before > 0, only entries published at or before
// that unix timestamp are touched; otherwise every unread entry is. It
// returns how many entries were marked.
func (s *Store) MarkFeedReadBefore(ctx context.Context, userID, feedID int64, before int64, now time.Time) (int, error) {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: mark feed %d read: begin tx: %w", feedID, err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE user_entry_state SET read_at = ?
		WHERE user_id = ? AND feed_id = ? AND read_at IS NULL AND (? <= 0 OR published_at <= ?)
	`, now.Unix(), userID, feedID, before, before)
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
