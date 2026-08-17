package api

import (
	"net/http"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// statsResponse embeds store.Stats (the DB-derived gauges) plus the
// crawler's in-memory internal-error ring buffer -- feedla-side crawl
// failures (store writes, typically) are deliberately kept off
// error_count/last_error (see crawler.go's crawlOne), so RecentInternalErrors
// is the only place they'd otherwise surface at all.
type statsResponse struct {
	store.Stats
	InternalErrors []crawler.InternalErrorEntry `json:"internal_errors"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	stats, err := s.store.GetStats(r.Context(), u.ID, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	subscribed, err := s.store.SubscribedFeedIDs(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// The ring buffer is process-wide (every user's crawls land in it), so
	// it needs the same "自分が購読している feed に限定" scoping GetStats
	// already applies at the SQL level (docs/multi-user-design.md's feeds-
	// sharing section) -- otherwise any authenticated user could read
	// internal error details about feeds only someone else subscribes to.
	all := s.crawler.RecentInternalErrors()
	internalErrors := make([]crawler.InternalErrorEntry, 0, len(all))
	for _, e := range all {
		if subscribed[e.FeedID] {
			internalErrors = append(internalErrors, e)
		}
	}

	writeJSON(w, http.StatusOK, statsResponse{
		Stats:          stats,
		InternalErrors: internalErrors,
	})
}
