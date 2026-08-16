// Package api implements feedla's HTTP API: a new /api/v1/* JSON API plus a
// Fastladder-compatible /api/* surface (see ldr.go) so existing LDR clients
// can be pointed at feedla.
package api

import (
	"net/http"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/metrics"
	"github.com/tokuhirom/feedla/internal/store"
)

// Server holds the dependencies every handler needs.
type Server struct {
	store   *store.Store
	crawler *crawler.Crawler
	fetcher *crawler.Fetcher
	metrics *metrics.Metrics
}

// NewHandler builds feedla's full HTTP API as a single http.Handler. m may
// be nil (e.g. in tests that don't care about /metrics), in which case
// GET /metrics reports empty fetch counters.
func NewHandler(st *store.Store, cr *crawler.Crawler, fetcher *crawler.Fetcher, m *metrics.Metrics) http.Handler {
	if m == nil {
		m = metrics.New()
	}
	s := &Server{store: st, crawler: cr, fetcher: fetcher, metrics: m}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	mux.HandleFunc("GET /api/v1/subscriptions", s.handleListSubscriptions)
	mux.HandleFunc("POST /api/v1/subscriptions", s.handleCreateSubscription)
	mux.HandleFunc("PATCH /api/v1/subscriptions/{id}", s.handlePatchSubscription)
	mux.HandleFunc("DELETE /api/v1/subscriptions/{id}", s.handleDeleteSubscription)
	mux.HandleFunc("GET /api/v1/subscriptions/{id}/entries", s.handleListEntries)
	mux.HandleFunc("POST /api/v1/subscriptions/{id}/read_all", s.handleReadAll)
	mux.HandleFunc("POST /api/v1/subscriptions/{id}/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/v1/entries/read", s.handleMarkEntriesRead)
	mux.HandleFunc("GET /api/v1/entries", s.handleListGroupEntries)
	mux.HandleFunc("GET /api/v1/entries/today", s.handleListTodayEntries)
	mux.HandleFunc("GET /api/v1/folders", s.handleListFolders)
	mux.HandleFunc("POST /api/v1/folders", s.handleCreateFolder)
	mux.HandleFunc("GET /api/v1/search", s.handleSearch)
	mux.HandleFunc("GET /api/v1/pins", s.handleListPins)
	mux.HandleFunc("POST /api/v1/pins", s.handleAddPin)
	mux.HandleFunc("DELETE /api/v1/pins/{id}", s.handleRemovePin)
	mux.HandleFunc("POST /api/v1/scrape_sources", s.handleCreateScrapeSource)
	mux.HandleFunc("GET /api/v1/scrape_sources", s.handleListScrapeSources)
	mux.HandleFunc("GET /api/v1/scrape_sources/{id}", s.handleGetScrapeSource)
	mux.HandleFunc("PATCH /api/v1/scrape_sources/{id}", s.handlePatchScrapeSource)
	mux.HandleFunc("POST /api/v1/scrape_sources/{id}/preview", s.handlePreviewScrapeSource)

	mux.HandleFunc("GET /api/v1/opml", s.handleExportOPML)
	mux.HandleFunc("POST /api/v1/opml", s.handleImportOPML)
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)
	mux.HandleFunc("GET /api/v1/ignore_words", s.handleListIgnoreWords)
	mux.HandleFunc("POST /api/v1/ignore_words", s.handleAddIgnoreWord)
	mux.HandleFunc("DELETE /api/v1/ignore_words/{id}", s.handleRemoveIgnoreWord)

	mux.HandleFunc("POST /api/subs", s.handleLDRSubs)
	mux.HandleFunc("POST /api/unread", s.handleLDRUnread)
	mux.HandleFunc("POST /api/touch_all", s.handleLDRTouchAll)
	mux.HandleFunc("POST /api/subscribe", s.handleLDRSubscribe)
	mux.HandleFunc("POST /api/unsubscribe", s.handleLDRUnsubscribe)
	mux.HandleFunc("POST /api/folders", s.handleLDRFolders)
	mux.HandleFunc("POST /api/pin/add", s.handleLDRPinAdd)
	mux.HandleFunc("POST /api/pin/remove", s.handleLDRPinRemove)
	mux.HandleFunc("POST /api/pin/all", s.handleLDRPinAll)

	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
