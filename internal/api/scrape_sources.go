package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/extract"
	"github.com/tokuhirom/feedla/internal/extract/pagewatch"
	"github.com/tokuhirom/feedla/internal/extract/selector"
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
// CreatedBy is included (unlike a typical "who made this" field elsewhere)
// so the Web UI can tell whether the current user may PATCH/preview this
// source and disable the settings form instead of letting a 404 be the
// first sign a non-owner can't save (§9.3 of the selector design).
type scrapeSourceView struct {
	ID        int64           `json:"id"`
	FeedID    int64           `json:"feed_id"`
	Kind      string          `json:"kind"`
	TargetURL string          `json:"target_url"`
	Config    json.RawMessage `json:"config"`
	CreatedBy int64           `json:"created_by"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

func toScrapeSourceView(src store.ScrapeSource) scrapeSourceView {
	return scrapeSourceView{
		ID: src.ID, FeedID: src.FeedID, Kind: src.Kind, TargetURL: src.TargetURL,
		Config: src.Config, CreatedBy: src.CreatedBy, CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt,
	}
}

type createScrapeSourceRequest struct {
	Kind     string          `json:"kind,omitempty"` // defaults to "pagewatch"
	URL      string          `json:"url"`
	FolderID *int64          `json:"folder_id,omitempty"`
	Title    string          `json:"title,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// validateScrapeConfig validates raw against kind's own Config type. The
// returned error's message is safe to send to the client as-is (400).
func validateScrapeConfig(kind string, raw json.RawMessage) error {
	switch kind {
	case string(extract.KindPageWatch):
		_, err := pagewatch.ParseConfig(raw)
		return err
	case string(extract.KindSelector):
		_, err := selector.ParseConfig(raw)
		return err
	default:
		return fmt.Errorf("unsupported kind: %s", kind)
	}
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
	if err := validateScrapeConfig(req.Kind, req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !checkActionQuota(w, s.feedAddLimiter, u.ID, "feed add") {
		return
	}
	subCount, err := s.store.CountSubscriptions(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !checkCountQuota(w, subCount, s.quota.MaxSubscriptions, "subscriptions") {
		return
	}
	srcCount, err := s.store.CountScrapeSources(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !checkCountQuota(w, srcCount, s.quota.MaxScrapeSources, "scrape sources") {
		return
	}

	now := time.Now()
	feedURL := crawler.PrefixForKind(extract.Kind(req.Kind)) + req.URL
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

	src, err := s.store.GetScrapeSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if src.CreatedBy != u.ID && !u.IsAdmin {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	// Validate against the saved kind's own Config type -- the request body
	// carries no kind, so a change of kind is never possible via PATCH.
	if err := validateScrapeConfig(src.Kind, req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
// what the source's currently-saved kind/config would extract from it --
// pagewatch's blocks (§8.1, §9.4) or selector's candidate items (§8.2) --
// so the UI can show the effect of an edit before it's crawled for real. It
// never touches scrape_sources.state and never diffs -- no side effects at
// all. Restricted to the scrape source's creator or an admin, same as
// handlePatchScrapeSource: it fetches an arbitrary URL on the caller's
// behalf (an SSRF-adjacent capability, per the design doc's resource-limits
// section), so it shouldn't be open to every subscriber.
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

	if !checkActionQuota(w, s.previewLimiter, u.ID, "preview") {
		return
	}

	body, status, err := s.fetchPreviewPage(r, src.TargetURL)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	switch src.Kind {
	case string(extract.KindPageWatch):
		cfg, err := pagewatch.ParseConfig(src.Config)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		blocks, err := pagewatch.Preview(src.TargetURL, body, cfg)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocks": blocks})
	case string(extract.KindSelector):
		cfg, err := selector.ParseConfig(src.Config)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		seen := map[string]bool{}
		if st, ok := selector.ParseState(src.State); ok {
			for _, u := range st.Seen {
				seen[u] = true
			}
		}
		result, err := selector.Preview(src.TargetURL, body, cfg, time.Now(), seen)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeError(w, http.StatusInternalServerError, "unsupported kind: "+src.Kind)
	}
}

// fetchPreviewPage performs the fetch+charset-decode common to both preview
// endpoints. On error it also returns the HTTP status the caller should
// respond with (always 502, since every failure here is "couldn't read the
// target page", never the caller's fault).
func (s *Server) fetchPreviewPage(r *http.Request, targetURL string) ([]byte, int, error) {
	fr, err := s.fetcher.Fetch(r.Context(), targetURL, crawler.FetchOptions{Accept: crawler.PagewatchAccept})
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	if fr.StatusCode != http.StatusOK {
		return nil, http.StatusBadGateway, fmt.Errorf("unexpected status fetching preview")
	}
	body, err := crawler.DecodeUTF8(fr.Body, fr.ContentType)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return body, 0, nil
}

type previewScrapeSourceRequest struct {
	Kind   string          `json:"kind,omitempty"` // defaults to "pagewatch"
	URL    string          `json:"url"`
	Config json.RawMessage `json:"config,omitempty"`
}

// handlePreviewUnsavedScrapeSource is the pre-subscribe preview endpoint
// (§8.2, POST /api/v1/scrape_sources/preview): unlike {id}/preview it takes
// url+config directly rather than a saved scrape source, since there's
// nothing to save yet while the user is still iterating on selectors.
// Ownership can't be checked -- there's no resource yet -- so
// authentication plus previewLimiter are the only guards (§8.2 calls this
// out explicitly as the most lightly-guarded arbitrary-URL-fetch endpoint
// in the API).
func (s *Server) handlePreviewUnsavedScrapeSource(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req previewScrapeSourceRequest
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
	if err := validateScrapeConfig(req.Kind, req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !checkActionQuota(w, s.previewLimiter, u.ID, "preview") {
		return
	}

	body, status, err := s.fetchPreviewPage(r, req.URL)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	switch req.Kind {
	case string(extract.KindPageWatch):
		cfg, _ := pagewatch.ParseConfig(req.Config)
		blocks, err := pagewatch.Preview(req.URL, body, cfg)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocks": blocks})
	case string(extract.KindSelector):
		cfg, _ := selector.ParseConfig(req.Config)
		result, err := selector.Preview(req.URL, body, cfg, time.Now(), nil)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
