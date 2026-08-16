package api

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// handleMetrics renders docs/DESIGN.md's `/metrics` in Prometheus text exposition
// format: fetch counters/histogram from s.metrics (process-lifetime,
// accumulated by the crawler) plus gauges computed live from the store.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// authMiddleware's FR_METRICS_TOKEN bypass never puts a user in
	// context (unlike the session/API-token paths) -- it's an ambient
	// ops-monitoring credential, not tied to any one account. Fall back to
	// the bootstrap admin (id=1) in that case; ok is only false via that
	// bypass since every other path through the middleware guarantees a
	// user (see userFromContext's doc comment).
	const bootstrapAdminID = 1
	userID := int64(bootstrapAdminID)
	if u, ok := userFromContext(r.Context()); ok {
		userID = u.ID
	}
	stats, err := s.store.GetStats(r.Context(), userID, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err := s.metrics.WriteFetchMetrics(w); err != nil {
		return // headers/body already partially written; nothing left to do
	}
	writeGauge(w, "feeds_total", "Total known feeds.", stats.FeedsTotal)
	writeGauge(w, "feeds_erroring", "Feeds with a non-zero consecutive error count.", stats.FeedsErroring)
	writeGauge(w, "entries_unread", "Total unread entries across all subscriptions.", stats.EntriesUnread)
	writeGauge(w, "queue_depth", "Feeds currently due for a crawl.", stats.QueueDepth)
	writeGauge(w, "db_size_bytes", "SQLite database file size in bytes.", stats.DBSizeBytes)
	writeGauge(w, "crawler_internal_errors_recent",
		"Feedla-side crawl failures (store writes, typically) currently held in the in-memory ring buffer -- see GET /api/v1/stats.",
		int64(len(s.crawler.RecentInternalErrors())))
}

func writeGauge(w io.Writer, name, help string, value int64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
}
