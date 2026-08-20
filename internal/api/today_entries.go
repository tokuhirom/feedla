package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// todayWindow is how far back "Today" looks.
const todayWindow = 24 * time.Hour

// handleListTodayEntries backs the sidebar's pinned "Today" group
// (プライオリティ表示のみ): 過去24時間に新規登録された未読記事を全フィード
// 横断で返す。GET /api/v1/entries/today?limit=&cursor=
func (s *Server) handleListTodayEntries(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	cursor := parseEntryCursor(q.Get("cursor"))

	since := time.Now().Add(-todayWindow).Unix()
	entries, err := s.store.ListTodayEntries(r.Context(), u.ID, since, limit, cursor)
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
