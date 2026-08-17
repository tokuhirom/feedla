package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/extract"
	"github.com/tokuhirom/feedla/internal/extract/pagewatch"
	"github.com/tokuhirom/feedla/internal/store"
)

// pagewatchDefaultIntervalSec is the initial crawl interval for a newly
// registered scrape source (§10.1 of the pagewatch design) -- deliberately
// looser than defaultFetchIntervalSec, since a site being scraped hasn't
// opted into being polled the way a feed publisher has.
const pagewatchDefaultIntervalSec = 3600

// scrapeSourceView is store.ScrapeSource minus State: state is
// feedla-internal bookkeeping (§6.7 -- "設定は残すべきデータ、state は捨てて
// よいデータ"), not something the API/backup surface needs to expose.
type scrapeSourceView struct {
	ID        int64           `json:"id"`
	FeedID    int64           `json:"feed_id"`
	Kind      string          `json:"kind"`
	TargetURL string          `json:"target_url"`
	Config    json.RawMessage `json:"config"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

func toScrapeSourceView(src store.ScrapeSource) scrapeSourceView {
	return scrapeSourceView{
		ID: src.ID, FeedID: src.FeedID, Kind: src.Kind, TargetURL: src.TargetURL,
		Config: src.Config, CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt,
	}
}

type createScrapeSourceRequest struct {
	Kind     string          `json:"kind,omitempty"` // defaults to "pagewatch", the only kind F0 supports
	URL      string          `json:"url"`
	FolderID *int64          `json:"folder_id,omitempty"`
	Title    string          `json:"title,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// handleCreateScrapeSource registers a page-watch subscription: unlike
// POST /api/v1/subscriptions, the caller is asserting url has no feed and
// asking feedla to watch the page itself (see docs/
// feedless-site-subscription-pagewatch.md §8.2 -- discover's "not found"
// behavior is intentionally left unchanged; the UI offers this as an
// explicit follow-up action instead of an automatic fallback).
func (s *Server) handleCreateScrapeSource(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req createScrapeSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Kind == "" {
		req.Kind = string(extract.KindPageWatch)
	}
	if req.Kind != string(extract.KindPageWatch) {
		writeError(w, http.StatusBadRequest, "unsupported kind: "+req.Kind)
		return
	}
	if _, err := pagewatch.ParseConfig(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now()
	feedURL := crawler.ScrapePrefix + req.URL
	feedID, err := s.store.UpsertFeed(r.Context(), feedURL, "", req.Title, pagewatchDefaultIntervalSec, now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.UpsertSubscription(r.Context(), u.ID, feedID, req.FolderID, req.Title, now); err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.store.CreateScrapeSource(r.Context(), u.ID, feedID, req.Kind, req.URL, req.Config, now); err != nil {
		writeStoreError(w, err)
		return
	}

	if _, err := s.crawler.CrawlFeed(r.Context(), feedID); err != nil {
		// A failed first crawl doesn't fail the subscribe, matching
		// handleCreateSubscription: the source is saved and the scheduler
		// retries it on its own schedule.
		slog.Warn("api: initial crawl after scrape source create failed", "feed_id", feedID, "error", err)
	}

	view, err := s.store.GetSubscriptionView(r.Context(), u.ID, feedID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createSubscriptionResponse{Subscription: &view})
}

func (s *Server) handleListScrapeSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListScrapeSources(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	views := make([]scrapeSourceView, len(sources))
	for i, src := range sources {
		views[i] = toScrapeSourceView(src)
	}
	writeJSON(w, http.StatusOK, map[string]any{"scrape_sources": views})
}

func (s *Server) handleGetScrapeSource(w http.ResponseWriter, r *http.Request) {
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	src, err := s.store.GetScrapeSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toScrapeSourceView(src))
}

type patchScrapeSourceRequest struct {
	Config json.RawMessage `json:"config"`
}

// handlePatchScrapeSource restricts config changes to the source's creator
// or an admin (docs/multi-user-design.md §scrape_sources: a config edit
// affects every subscriber, so it's not open to whoever happens to be
// subscribed). A non-owner, non-admin caller gets the same 404 as a
// genuinely missing id, per the design doc's "don't let 403 confirm
// existence" IDOR guidance.
func (s *Server) handlePatchScrapeSource(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req patchScrapeSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := pagewatch.ParseConfig(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	src, err := s.store.GetScrapeSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if src.CreatedBy != u.ID && !u.IsAdmin {
		writeStoreError(w, store.ErrNotFound)
		return
	}

	if err := s.store.UpdateScrapeSourceConfig(r.Context(), id, req.Config, time.Now()); err != nil {
		writeStoreError(w, err)
		return
	}
	src, err = s.store.GetScrapeSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toScrapeSourceView(src))
}

// handlePreviewScrapeSource fetches id's target URL right now and returns
// the blocks pagewatch would extract under its currently-saved config, so
// the UI can show which blocks an ignore_patterns edit would hide (§8.1,
// §9.4). It never touches scrape_sources.state and never diffs -- no side
// effects at all. Restricted to the scrape source's creator or an admin,
// same as handlePatchScrapeSource: it fetches an arbitrary URL on the
// caller's behalf (an SSRF-adjacent capability, per the design doc's
// resource-limits section), so it shouldn't be open to every subscriber.
func (s *Server) handlePreviewScrapeSource(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	src, err := s.store.GetScrapeSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if src.CreatedBy != u.ID && !u.IsAdmin {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	cfg, err := pagewatch.ParseConfig(src.Config)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	fr, err := s.fetcher.Fetch(r.Context(), src.TargetURL, crawler.FetchOptions{Accept: crawler.PagewatchAccept})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if fr.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "unexpected status fetching preview")
		return
	}
	body, err := crawler.DecodeUTF8(fr.Body, fr.ContentType)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	blocks, err := pagewatch.Preview(src.TargetURL, body, cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"blocks": blocks})
}
