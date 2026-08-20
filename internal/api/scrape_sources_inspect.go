package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/inspect"
)

type inspectScrapeSourceRequest struct {
	URL string `json:"url"`
}

type inspectScrapeSourceResponse struct {
	ViewURL   string            `json:"view_url"`
	Elements  []inspect.Element `json:"elements"`
	ExpiresAt int64             `json:"expires_at"`
}

// handleInspectScrapeSource is F2's first half (§8.3, §10.3 of
// docs/feedless-site-subscription-selector.md): fetch url on the caller's
// behalf, reduce it to an allow-listed, safe-to-display subset, and hand
// back a short-lived, single-use token the frontend points an iframe at
// (GET .../inspect/view, handleInspectView below) instead of embedding the
// third-party HTML directly.
//
// This stays behind the normal auth+quota gate that
// handlePreviewUnsavedScrapeSource uses -- it triggers an arbitrary fetch,
// same SSRF-adjacent shape as preview (§8.2), and ownership can't be
// checked for the same reason (no resource exists yet).
func (s *Server) handleInspectScrapeSource(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())

	var req inspectScrapeSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	if !checkActionQuota(w, s.previewLimiter, u.ID, "inspect") {
		return
	}

	body, status, err := s.fetchPreviewPage(r, req.URL)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	html, elements := inspect.Sanitize(body)
	now := time.Now()
	token, err := s.inspectTokens.Issue(u.ID, html, elements, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue inspect token")
		return
	}

	writeJSON(w, http.StatusOK, inspectScrapeSourceResponse{
		ViewURL:   "/api/v1/scrape_sources/inspect/view?t=" + token,
		Elements:  elements,
		ExpiresAt: now.Add(inspect.TTL).Unix(),
	})
}

// handleInspectView serves the sanitized page built by
// handleInspectScrapeSource, for direct iframe navigation.
//
// It is registered in authMiddleware's publicPaths (see api.go/
// auth_middleware.go) and bypasses the normal session/API-token gate
// entirely. That is required, not an oversight: per §8.3/§10.3, the
// sandboxed iframe reading this URL back may not send a SameSite session
// cookie at all, so gating on it would 401 the legitimate flow. The
// single-use, five-minute, 256-bit token is the sole authorization --
// internal/inspect.TokenStore.Consume documents the same contract from the
// store's side.
//
// As defense in depth, if a session cookie *is* present (a browser tab
// opened directly on this URL, rather than the sandboxed iframe), its
// owner is compared against the token's issuer and mismatches are
// rejected -- see the authenticateSession call below.
func (s *Server) handleInspectView(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	entry, ok := s.inspectTokens.Consume(token, time.Now())
	if !ok {
		writeError(w, http.StatusNotFound, "inspect token not found or expired")
		return
	}
	if u, ok := s.authenticateSession(r); ok && u.ID != entry.UserID {
		writeError(w, http.StatusNotFound, "inspect token not found or expired")
		return
	}

	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'sha256-"+inspect.PickerScriptSHA256+"'; sandbox allow-scripts; img-src data:")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(entry.HTML)
}
