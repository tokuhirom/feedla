package store

import (
	"context"
	"fmt"
)

// GetOrCreateFolder returns the id of the folder with the given name,
// creating it if it doesn't exist yet.
func (s *Store) GetOrCreateFolder(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.Write.QueryRowContext(ctx, `
		INSERT INTO folders(name) VALUES (?)
		ON CONFLICT(name) DO UPDATE SET name = excluded.name
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: get or create folder %q: %w", name, err)
	}
	return id, nil
}

// ListFolders returns every folder ordered for display.
func (s *Store) ListFolders(ctx context.Context) ([]Folder, error) {
	rows, err := s.Read.QueryContext(ctx, `
		SELECT id, name, sort_order FROM folders ORDER BY sort_order, name
	`)
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
