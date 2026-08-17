package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

// defaultFetchIntervalSec mirrors internal/feed.ImportOPML's default: the
// initial crawl interval for a newly subscribed feed, before adaptive
// scheduling (see internal/crawler/backoff.go) kicks in.
const defaultFetchIntervalSec = 1800

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	views, err := s.store.ListSubscriptionViews(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if views == nil {
		views = []store.SubscriptionView{}
	}
	todayCount, err := s.store.CountTodayUnread(r.Context(), u.ID, time.Now().Add(-todayWindow).Unix())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptions":      views,
		"today_unread_count": todayCount,
	})
}

type createSubscriptionRequest struct {
	URL      string `json:"url"`
	FolderID *int64 `json:"folder_id,omitempty"`
	Title    string `json:"title,omitempty"`
}

type createSubscriptionResponse struct {
	Subscription *store.SubscriptionView `json:"subscription,omitempty"`
	Candidates   []feed.Candidate        `json:"candidates,omitempty"`
}

// handleCreateSubscription resolves req.URL to a feed (see
// internal/feed.DiscoverFeed), subscribes to it, and crawls it once
// immediately so the caller can fetch entries right away without waiting
// for the scheduler. If the URL is an HTML page linking multiple feeds, it
// returns 202 with the candidate list instead of guessing.
func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req createSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.FolderID != nil && *req.FolderID != 0 {
		if _, err := s.store.GetFolder(r.Context(), u.ID, *req.FolderID); err != nil {
			writeStoreError(w, err)
			return
		}
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

	candidates, err := feed.DiscoverFeed(r.Context(), s.fetcher, req.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(candidates) > 1 {
		writeJSON(w, http.StatusAccepted, createSubscriptionResponse{Candidates: candidates})
		return
	}
	chosen := candidates[0]

	now := time.Now()
	title := req.Title
	if title == "" {
		title = chosen.Title
	}

	feedID, err := s.store.UpsertFeed(r.Context(), chosen.FeedURL, "", chosen.Title, defaultFetchIntervalSec, now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.UpsertSubscription(r.Context(), u.ID, feedID, req.FolderID, title, now); err != nil {
		writeStoreError(w, err)
		return
	}

	if _, err := s.crawler.CrawlFeed(r.Context(), feedID); err != nil {
		// A failed first crawl doesn't fail the subscribe: the feed is
		// saved and the scheduler will retry it on its own schedule.
		slog.Warn("api: initial crawl after subscribe failed", "feed_id", feedID, "error", err)
	}

	view, err := s.store.GetSubscriptionView(r.Context(), u.ID, feedID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createSubscriptionResponse{Subscription: &view})
}

type patchSubscriptionRequest struct {
	Title    *string `json:"title"`
	Rating   *int64  `json:"rating"`
	FolderID *int64  `json:"folder_id"`
}

func (s *Server) handlePatchSubscription(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req patchSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// A non-nil FolderID of 0 means "clear the folder" (store.
	// SubscriptionPatch's convention); only a non-zero id needs an
	// ownership check against userID's own folders.
	if req.FolderID != nil && *req.FolderID != 0 {
		if _, err := s.store.GetFolder(r.Context(), u.ID, *req.FolderID); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	patch := store.SubscriptionPatch{Title: req.Title, Rating: req.Rating, FolderID: req.FolderID}
	if err := s.store.UpdateSubscription(r.Context(), u.ID, id, patch); err != nil {
		writeStoreError(w, err)
		return
	}

	view, err := s.store.GetSubscriptionView(r.Context(), u.ID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.Unsubscribe(r.Context(), u.ID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	q := r.URL.Query()
	unreadOnly := q.Get("unread") == "1"
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	cursor := parseEntryCursor(q.Get("cursor"))

	entries, err := s.store.ListEntries(r.Context(), u.ID, id, unreadOnly, limit, cursor)
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

func parseEntryCursor(v string) *store.EntryCursor {
	parts := strings.SplitN(v, ",", 2)
	if len(parts) != 2 {
		return nil
	}
	pub, err1 := strconv.ParseInt(parts[0], 10, 64)
	id, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	return &store.EntryCursor{PublishedAt: pub, ID: id}
}

func formatEntryCursor(publishedAt, id int64) string {
	return strconv.FormatInt(publishedAt, 10) + "," + strconv.FormatInt(id, 10)
}

func (s *Server) handleReadAll(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Before int64 `json:"before"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	n, err := s.store.MarkFeedReadBefore(r.Context(), u.ID, id, req.Before, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}

// handleRefresh triggers an immediate crawl of a feed the caller
// subscribes to. Per docs/multi-user-design.md's §リソース制限 note that a
// manual refresh forces a crawl of a feed shared across all its
// subscribers, this is limited to the caller's own subscriptions (an
// arbitrary feed_id would otherwise let any authenticated user force-crawl
// any feed in the system) and rate-limited per user.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	subscribed, err := s.store.IsSubscribed(r.Context(), u.ID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !subscribed {
		writeStoreError(w, store.ErrNotFound)
		return
	}

	if !checkActionQuota(w, s.refreshLimiter, u.ID, "refresh") {
		return
	}

	res, err := s.crawler.CrawlFeed(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := map[string]any{"new_entries": res.NewEntries}
	if res.Err != nil {
		resp["error"] = res.Err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMarkEntriesRead(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req struct {
		EntryIDs []int64 `json:"entry_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	n, err := s.store.MarkEntriesRead(r.Context(), u.ID, req.EntryIDs, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}

func (s *Server) handleMarkAllEntriesRead(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	n, err := s.store.MarkAllEntriesRead(r.Context(), u.ID, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}
