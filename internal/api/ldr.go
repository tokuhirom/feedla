// Fastladder-compatible endpoints: POST, form-encoded requests, JSON
// responses, so existing LDR/Fastladder clients can point at feedla. New
// features live under /api/v1/* instead (see subscriptions.go); this file
// only covers the subset feedla's schema can support 1:1 — subscribe_id
// here is always a feed id, since a Subscription is 1:1 with its Feed.
package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/tokuhirom/feedla/internal/feed"
	"github.com/tokuhirom/feedla/internal/store"
)

func (s *Server) handleLDRSubs(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	unreadOnly := r.FormValue("unread") == "1"

	views, err := s.store.ListSubscriptionViews(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]store.SubscriptionView, 0, len(views))
	for _, v := range views {
		if unreadOnly && v.UnreadCount == 0 {
			continue
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLDRUnread(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}

	entries, err := s.store.ListEntries(r.Context(), id, true, 500, nil)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if entries == nil {
		entries = []store.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleLDRTouchAll(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}

	n, err := s.store.MarkFeedReadBefore(r.Context(), id, 0, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}

func (s *Server) handleLDRSubscribe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	feedLink := r.FormValue("feedlink")
	if feedLink == "" {
		writeError(w, http.StatusBadRequest, "feedlink is required")
		return
	}

	candidates, err := feed.DiscoverFeed(r.Context(), s.fetcher, feedLink)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	chosen := candidates[0]

	now := time.Now()
	feedID, err := s.store.UpsertFeed(r.Context(), chosen.FeedURL, "", chosen.Title, defaultFetchIntervalSec, now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.UpsertSubscription(r.Context(), feedID, nil, chosen.Title, now); err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.crawler.CrawlFeed(r.Context(), feedID); err != nil {
		slog.Warn("api: initial crawl after ldr subscribe failed", "feed_id", feedID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"subscribe_id": feedID})
}

func (s *Server) handleLDRUnsubscribe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}
	if err := s.store.DeleteFeed(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLDRFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.store.ListFolders(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if folders == nil {
		folders = []store.Folder{}
	}
	writeJSON(w, http.StatusOK, folders)
}
