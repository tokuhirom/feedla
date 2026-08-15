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
	stats, err := s.store.GetStats(r.Context(), time.Now())
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
