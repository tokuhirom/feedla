package api

import (
	"net/http"
	"strconv"

	"github.com/tokuhirom/feedla/internal/store"
)

// handleListGroupEntries backs "read everything in this folder/priority
// level at once": GET /api/v1/entries?folder_id=&unread=&limit=&cursor= or
// GET /api/v1/entries?rating=&... (exactly one of folder_id/rating).
// folder_id=0 means the unfiled bucket, matching the sidebar's convention
// for subscriptions with no folder.
func (s *Server) handleListGroupEntries(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	q := r.URL.Query()
	unreadOnly := q.Get("unread") == "1"
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	cursor := parseEntryCursor(q.Get("cursor"))

	folderRaw := q.Get("folder_id")
	ratingRaw := q.Get("rating")
	if (folderRaw == "") == (ratingRaw == "") {
		writeError(w, http.StatusBadRequest, "specify exactly one of folder_id or rating")
		return
	}

	var (
		entries []store.Entry
		err     error
	)
	if folderRaw != "" {
		folderID, perr := strconv.ParseInt(folderRaw, 10, 64)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid folder_id")
			return
		}
		var folderPtr *int64
		if folderID != 0 {
			folderPtr = &folderID
		}
		entries, err = s.store.ListEntriesByFolder(r.Context(), u.ID, folderPtr, unreadOnly, limit, cursor)
	} else {
		rating, perr := strconv.ParseInt(ratingRaw, 10, 64)
		if perr != nil || rating < 0 || rating > 5 {
			writeError(w, http.StatusBadRequest, "invalid rating")
			return
		}
		entries, err = s.store.ListEntriesByRating(r.Context(), u.ID, rating, unreadOnly, limit, cursor)
	}
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
