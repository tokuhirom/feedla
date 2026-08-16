package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tokuhirom/feedla/internal/store"
)

// handleSearch full-text searches entries across every feed. See
// internal/store.SearchEntries for the trigram/LIKE fallback split.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	cursor := parseEntryCursor(q.Get("cursor"))

	entries, err := s.store.SearchEntries(r.Context(), u.ID, query, limit, cursor)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if entries == nil {
		entries = []store.Entry{}
	}

	resp := struct {
		Entries    []store.Entry `json:"entries"`
		NextCursor string        `json:"next_cursor,omitempty"`
	}{Entries: entries}
	if len(entries) == limit {
		last := entries[len(entries)-1]
		resp.NextCursor = formatEntryCursor(last.PublishedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}
