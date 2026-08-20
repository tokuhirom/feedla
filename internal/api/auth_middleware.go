package api

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/store"
)

// parseOrigin extracts the host from an Origin header value (e.g.
// "https://example.com" -> "example.com").
func parseOrigin(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "", err
	}
	return u.Host, nil
}

// sessionIdleTimeout is the "not used in this long" cutoff from
// docs/multi-user-design.md (30 days), checked against sessions.last_seen_at
// independently of the cookie's/session's absolute expiry.
const sessionIdleTimeout = 30 * 24 * time.Hour

// touchThrottle is the minimum gap between last_seen_at / last_used_at
// writes for the same session/token, so a busy client doesn't turn every
// request into a write-conn transaction.
const touchThrottle = time.Hour

// publicPaths lists the exact "METHOD path" pairs the auth middleware lets
// through without a session/token -- everything else is protected by
// default (opt-out, not opt-in), per docs/multi-user-design.md.
var publicPaths = map[string]bool{
	"GET /healthz":            true,
	"POST /api/v1/auth/login": true,
	"POST /api/v1/auth/setup": true,
	// /auth/restore is the setup screen's "restore from backup instead"
	// choice; like /auth/setup it's only meaningful (and only succeeds)
	// while no admin account exists yet, so there's no session to require.
	"POST /api/v1/auth/restore": true,
	// /auth/me must work both logged-out (to report setup_required so the
	// SPA knows whether to show the setup or login screen) and logged-in
	// (to report the current user), so it resolves auth itself rather than
	// being gated by this middleware -- see handleAuthMe.
	"GET /api/v1/auth/me": true,
	// Invitation status/accept are reached by a visitor who has no session
	// yet (they're following an admin-issued link) -- see
	// handleInvitationStatus/handleAcceptInvitation.
	"POST /api/v1/invitations/status": true,
	"POST /api/v1/invitations/accept": true,
	// inspect/view is read by a sandboxed iframe with no allow-same-origin
	// (Phase F2's safe third-party-page display, §10.3 of
	// docs/feedless-site-subscription-selector.md), which may not send a
	// SameSite session cookie at all. Its own single-use, five-minute,
	// 256-bit token (see handleInspectView / internal/inspect.TokenStore)
	// is the sole authorization -- gating this route on a session the
	// caller might not have would 401 the legitimate flow.
	"GET /api/v1/scrape_sources/inspect/view": true,
}

// jsonBodyExempt lists /api/v1/* POST/PATCH routes whose body is
// legitimately not JSON, so the Content-Type: application/json check below
// doesn't apply to them.
var jsonBodyExempt = map[string]bool{
	"POST /api/v1/opml": true, // text/x-opml
}

// authMiddleware is feedla's single authentication + CSRF gate, replacing
// the pre-auth checkOrigin (see docs/multi-user-design.md's CSRF 対策の
// 作り直し section for the design this implements):
//
//  1. A small allowlist of routes (login, setup, healthz) is public.
//  2. GET /metrics accepts FR_METRICS_TOKEN as a Bearer token in addition
//     to the normal session/API-token auth below.
//  3. Authorization: Bearer <token> or a Fastladder-compatible `ApiKey`
//     form/query parameter is checked first. A match authenticates the
//     request without any CSRF check: a cross-site attacker can't set
//     Authorization headers or know a user's API token, so there's no
//     ambient authority to protect here.
//  4. Otherwise, a session cookie is checked. A valid, non-expired,
//     non-idle session authenticates the request, but state-changing
//     methods additionally require a matching Origin header (SameSite=Lax
//     is the first layer of defense; this is the second).
//  5. Anything else is 401.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if publicPaths[key] {
			next.ServeHTTP(w, r)
			return
		}

		if r.URL.Path == "/metrics" && r.Method == http.MethodGet && s.metricsToken != "" {
			if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				if subtle.ConstantTimeCompare([]byte(bearer), []byte(s.metricsToken)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		if u, ok := s.authenticateToken(r); ok {
			next.ServeHTTP(w, r.WithContext(contextWithUser(r.Context(), u)))
			return
		}

		u, ok := s.authenticateSession(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if !isSafeMethod(r.Method) {
			if !s.checkOrigin(r) {
				writeError(w, http.StatusForbidden, "cross-origin request rejected")
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/v1/") &&
				(r.Method == http.MethodPost || r.Method == http.MethodPatch) &&
				!jsonBodyExempt[key] &&
				!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "expected application/json")
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(contextWithUser(r.Context(), u)))
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// authenticateToken checks Authorization: Bearer and the Fastladder-
// compatible ApiKey parameter, in that order. Non-browser clients use this
// path, so it's exempt from the Origin/CSRF check applied to cookies.
func (s *Server) authenticateToken(r *http.Request) (store.User, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		_ = r.ParseForm() // no-ops the body read for non-form content types; see auth_middleware doc.
		raw = r.Form.Get("ApiKey")
		if raw == "" {
			return store.User{}, false
		}
	}

	tok, err := s.store.GetAPITokenByHash(r.Context(), auth.HashToken(raw))
	if err != nil {
		return store.User{}, false
	}

	if tok.LastUsedAt == nil || time.Unix(*tok.LastUsedAt, 0).Before(s.now().Add(-touchThrottle)) {
		_ = s.store.TouchAPITokenLastUsed(r.Context(), tok.ID, s.now())
	}

	return tok.User, true
}

// authenticateSession checks the session cookie, enforcing idle + absolute
// expiry. It never writes an error response itself; the caller decides
// whether to fall through or reject.
func (s *Server) authenticateSession(r *http.Request) (store.User, bool) {
	raw, ok := sessionCookieFromRequest(r)
	if !ok {
		return store.User{}, false
	}

	sess, err := s.store.GetSessionByTokenHash(r.Context(), auth.HashToken(raw))
	if err != nil {
		return store.User{}, false
	}

	now := s.now()
	if now.After(time.Unix(sess.ExpiresAt, 0)) {
		return store.User{}, false
	}
	if now.Sub(time.Unix(sess.LastSeenAt, 0)) > sessionIdleTimeout {
		return store.User{}, false
	}

	if now.Sub(time.Unix(sess.LastSeenAt, 0)) > touchThrottle {
		_ = s.store.TouchSession(r.Context(), sess.ID, now)
	}

	return sess.User, true
}

// checkOrigin requires a present, matching Origin header on cookie-
// authenticated state-changing requests. Unlike the old pre-auth
// checkOrigin, a missing Origin is now rejected too: once a session cookie
// is ambient authority, there's no non-browser-client case left to excuse
// (those use the token path above, which never reaches here).
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := parseOrigin(origin)
	if err != nil {
		return false
	}

	want := s.publicOrigin
	if want == "" {
		return strings.EqualFold(u, r.Host)
	}
	return strings.EqualFold(origin, want)
}
