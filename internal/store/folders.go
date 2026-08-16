package store

import (
	"context"
	"database/sql"
	"fmt"
)

// GetOrCreateFolder returns the id of userID's folder with the given name,
// creating it if it doesn't exist yet.
func (s *Store) GetOrCreateFolder(ctx context.Context, userID int64, name string) (int64, error) {
	var id int64
	err := s.Write.QueryRowContext(ctx, `
		INSERT INTO folders(user_id, name) VALUES (?, ?)
		ON CONFLICT(user_id, name) DO UPDATE SET name = excluded.name
		RETURNING id
	`, userID, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: get or create folder %q: %w", name, err)
	}
	return id, nil
}

// GetFolder returns userID's folder by id. Returns ErrNotFound if it
// doesn't exist or belongs to a different user.
func (s *Store) GetFolder(ctx context.Context, userID, folderID int64) (Folder, error) {
	var f Folder
	err := s.Read.QueryRowContext(ctx, `
		SELECT id, name, sort_order FROM folders WHERE id = ? AND user_id = ?
	`, folderID, userID).Scan(&f.ID, &f.Name, &f.SortOrder)
	if err == sql.ErrNoRows {
		return Folder{}, fmt.Errorf("store: get folder %d: %w", folderID, ErrNotFound)
	}
	if err != nil {
		return Folder{}, fmt.Errorf("store: get folder %d: %w", folderID, err)
	}
	return f, nil
}

// ListFolders returns every folder owned by userID, ordered for display.
func (s *Store) ListFolders(ctx context.Context, userID int64) ([]Folder, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, name, sort_order FROM folders WHERE user_id = ? ORDER BY sort_order, name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list folders: %w", err)
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.SortOrder); err != nil {
			return nil, fmt.Errorf("store: scan folder: %w", err)
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}
