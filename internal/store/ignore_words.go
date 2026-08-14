package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// IgnoreWord is a global substring filter: any entry whose title or body
// contains it is hidden from unread lists and excluded from unread_count.
type IgnoreWord struct {
	ID        int64  `json:"id"`
	Word      string `json:"word"`
	CreatedAt int64  `json:"created_at"`
}

// AddIgnoreWord registers word and immediately recomputes which already-
// fetched entries it now hides. A no-op if word is already registered.
func (s *Store) AddIgnoreWord(ctx context.Context, word string, now time.Time) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return fmt.Errorf("store: add ignore word: word is empty")
	}

	if _, err := s.Write.ExecContext(ctx, `
		INSERT INTO ignore_words(word, created_at) VALUES (?, ?)
		ON CONFLICT(word) DO NOTHING
	`, word, now.Unix()); err != nil {
		return fmt.Errorf("store: add ignore word %q: %w", word, err)
	}
	return s.recomputeIgnored(ctx)
}

// RemoveIgnoreWord deletes an ignore word and un-hides any entries that no
// longer match any remaining word. Returns ErrNotFound if id doesn't exist.
func (s *Store) RemoveIgnoreWord(ctx context.Context, id int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM ignore_words WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: remove ignore word %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: remove ignore word %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: remove ignore word %d: %w", id, ErrNotFound)
	}
	return s.recomputeIgnored(ctx)
}

// ListIgnoreWords returns every ignore word, newest first.
func (s *Store) ListIgnoreWords(ctx context.Context) ([]IgnoreWord, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, word, created_at FROM ignore_words ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list ignore words: %w", err)
	}
	defer rows.Close()

	var words []IgnoreWord
	for rows.Next() {
		var w IgnoreWord
		if err := rows.Scan(&w.ID, &w.Word, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan ignore word: %w", err)
		}
		words = append(words, w)
	}
	return words, rows.Err()
}

// recomputeIgnored re-evaluates entries.ignored against the current
// ignore_words list and refreshes every feed's unread_count to match, so
// adding or removing a word takes effect immediately for entries already
// fetched (not just ones seen from now on).
func (s *Store) recomputeIgnored(ctx context.Context) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: recompute ignored: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET ignored = EXISTS(
			SELECT 1 FROM ignore_words iw
			WHERE entries.title LIKE '%' || iw.word || '%' OR entries.body LIKE '%' || iw.word || '%'
		)
	`); err != nil {
		return fmt.Errorf("store: recompute ignored: update entries: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET unread_count = (
			SELECT COUNT(*) FROM entries WHERE feed_id = subscriptions.feed_id AND read_at IS NULL AND ignored = 0
		)
	`); err != nil {
		return fmt.Errorf("store: recompute ignored: refresh unread counts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: recompute ignored: commit: %w", err)
	}
	return nil
}
