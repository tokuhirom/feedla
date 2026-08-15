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
	stats, err := s.store.GetStats(r.Context(), time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{
		Stats:          stats,
		InternalErrors: s.crawler.RecentInternalErrors(),
	})
}
