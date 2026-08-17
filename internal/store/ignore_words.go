package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// IgnoreWord is a per-user substring filter: any entry whose title or body
// contains it is hidden from that user's unread lists and excluded from
// their unread_count.
type IgnoreWord struct {
	ID        int64  `json:"id"`
	Word      string `json:"word"`
	CreatedAt int64  `json:"created_at"`
}

// AddIgnoreWord registers word for userID and immediately recomputes which
// already-fetched entries it now hides for them. A no-op if word is already
// registered for userID.
func (s *Store) AddIgnoreWord(ctx context.Context, userID int64, word string, now time.Time) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return fmt.Errorf("store: add ignore word: word is empty")
	}

	if _, err := s.Write.ExecContext(ctx, `
		INSERT INTO ignore_words(user_id, word, created_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id, word) DO NOTHING
	`, userID, word, now.Unix()); err != nil {
		return fmt.Errorf("store: add ignore word %q: %w", word, err)
	}
	return s.recomputeIgnored(ctx, userID)
}

// RemoveIgnoreWord deletes userID's ignore word and un-hides any entries
// that no longer match any of their remaining words. Returns ErrNotFound if
// id doesn't exist or belongs to a different user.
func (s *Store) RemoveIgnoreWord(ctx context.Context, userID, id int64) error {
	res, err := s.Write.ExecContext(ctx, `DELETE FROM ignore_words WHERE id = ? AND user_id = ?`, id, userID)
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
	return s.recomputeIgnored(ctx, userID)
}

// CountIgnoreWords returns how many ignore words userID has registered, for
// enforcing the FR_QUOTA_MAX_IGNORE_WORDS limit.
func (s *Store) CountIgnoreWords(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM ignore_words WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count ignore words: %w", err)
	}
	return n, nil
}

// ListIgnoreWords returns every ignore word userID has registered, newest
// first.
func (s *Store) ListIgnoreWords(ctx context.Context, userID int64) ([]IgnoreWord, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, word, created_at FROM ignore_words WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
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

// recomputeIgnored re-evaluates userID's user_entry_state.ignored against
// their current ignore_words list and refreshes their unread_count to
// match, so adding or removing a word takes effect immediately for entries
// already fetched (not just ones seen from now on).
func (s *Store) recomputeIgnored(ctx context.Context, userID int64) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: recompute ignored: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE user_entry_state SET ignored = EXISTS(
			SELECT 1 FROM ignore_words iw
			JOIN entries e ON e.id = user_entry_state.entry_id
			WHERE iw.user_id = user_entry_state.user_id
			  AND (e.title LIKE '%' || iw.word || '%' OR e.body LIKE '%' || iw.word || '%')
		)
		WHERE user_id = ?
	`, userID); err != nil {
		return fmt.Errorf("store: recompute ignored: update user_entry_state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET unread_count = (
			SELECT COUNT(*) FROM user_entry_state ues
			WHERE ues.user_id = subscriptions.user_id AND ues.feed_id = subscriptions.feed_id
			  AND ues.read_at IS NULL AND ues.ignored = 0
		) WHERE user_id = ?
	`, userID); err != nil {
		return fmt.Errorf("store: recompute ignored: refresh unread counts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: recompute ignored: commit: %w", err)
	}
	return nil
}
