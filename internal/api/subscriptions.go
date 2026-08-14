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
	views, err := s.store.ListSubscriptionViews(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if views == nil {
		views = []store.SubscriptionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": views})
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
	var req createSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
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
	if err := s.store.UpsertSubscription(r.Context(), feedID, req.FolderID, title, now); err != nil {
		writeStoreError(w, err)
		return
	}

	if _, err := s.crawler.CrawlFeed(r.Context(), feedID); err != nil {
		// A failed first crawl doesn't fail the subscribe: the feed is
		// saved and the scheduler will retry it on its own schedule.
		slog.Warn("api: initial crawl after subscribe failed", "feed_id", feedID, "error", err)
	}

	view, err := s.store.GetSubscriptionView(r.Context(), feedID)
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

	patch := store.SubscriptionPatch{Title: req.Title, Rating: req.Rating, FolderID: req.FolderID}
	if err := s.store.UpdateSubscription(r.Context(), id, patch); err != nil {
		writeStoreError(w, err)
		return
	}

	view, err := s.store.GetSubscriptionView(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteFeed(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
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

	entries, err := s.store.ListEntries(r.Context(), id, unreadOnly, limit, cursor)
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

	n, err := s.store.MarkFeedReadBefore(r.Context(), id, req.Before, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
	var req struct {
		EntryIDs []int64 `json:"entry_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	n, err := s.store.MarkEntriesRead(r.Context(), req.EntryIDs, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}
