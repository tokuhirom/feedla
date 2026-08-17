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
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	unreadOnly := r.FormValue("unread") == "1"

	views, err := s.store.ListSubscriptionViews(r.Context(), u.ID)
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
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}

	entries, err := s.store.ListEntries(r.Context(), u.ID, id, true, 500, nil)
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
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}

	n, err := s.store.MarkFeedReadBefore(r.Context(), u.ID, id, 0, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": n})
}

func (s *Server) handleLDRSubscribe(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	feedLink := r.FormValue("feedlink")
	if feedLink == "" {
		writeError(w, http.StatusBadRequest, "feedlink is required")
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
	if err := s.store.UpsertSubscription(r.Context(), u.ID, feedID, nil, chosen.Title, now); err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.crawler.CrawlFeed(r.Context(), feedID); err != nil {
		slog.Warn("api: initial crawl after ldr subscribe failed", "feed_id", feedID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"subscribe_id": feedID})
}

func (s *Server) handleLDRUnsubscribe(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.FormValue("subscribe_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscribe_id")
		return
	}
	if err := s.store.Unsubscribe(r.Context(), u.ID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLDRFolders(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	folders, err := s.store.ListFolders(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if folders == nil {
		folders = []store.Folder{}
	}
	writeJSON(w, http.StatusOK, folders)
}

// handleLDRPinAdd resolves the "link" form field to an entry (LDR carries
// pins by link, not id) and pins it.
func (s *Server) handleLDRPinAdd(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	link := r.FormValue("link")
	if link == "" {
		writeError(w, http.StatusBadRequest, "link is required")
		return
	}

	entryID, err := s.store.FindEntryByURL(r.Context(), link)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	pinCount, err := s.store.CountPins(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !checkCountQuota(w, pinCount, s.quota.MaxPins, "pins") {
		return
	}
	if err := s.store.AddPin(r.Context(), u.ID, entryID, time.Now()); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLDRPinRemove(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	_ = r.ParseForm()
	link := r.FormValue("link")
	if link == "" {
		writeError(w, http.StatusBadRequest, "link is required")
		return
	}

	entryID, err := s.store.FindEntryByURL(r.Context(), link)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.RemovePin(r.Context(), u.ID, entryID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLDRPinAll(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	pins, err := s.store.ListPins(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if pins == nil {
		pins = []store.Pin{}
	}
	writeJSON(w, http.StatusOK, pins)
}
