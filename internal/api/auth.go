package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/store"
)

// minPasswordLen is the "at least 12 characters, no other complexity
// rules" policy from docs/multi-user-design.md (NIST SP 800-63B).
const minPasswordLen = 12

// sessionExpiry is the absolute session lifetime (90 days), matching
// sessionMaxAgeSeconds in cookie.go.
const sessionExpiry = 90 * 24 * time.Hour

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type userView struct {
	ID                     int64  `json:"id"`
	Username               string `json:"username"`
	IsAdmin                bool   `json:"is_admin"`
	InstagramEmbedsEnabled bool   `json:"instagram_embeds_enabled"`
}

func toUserView(u store.User) userView {
	return userView{
		ID:                     u.ID,
		Username:               u.Username,
		IsAdmin:                u.IsAdmin,
		InstagramEmbedsEnabled: u.InstagramEmbedsEnabled,
	}
}

type authMeResponse struct {
	Authenticated bool      `json:"authenticated"`
	SetupRequired bool      `json:"setup_required"`
	User          *userView `json:"user,omitempty"`
}

// handleAuthMe is public (see publicPaths in auth_middleware.go) because
// the SPA calls it on boot before knowing whether it's logged in at all.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.authenticateToken(r); ok {
		uv := toUserView(u)
		writeJSON(w, http.StatusOK, authMeResponse{Authenticated: true, User: &uv})
		return
	}
	if u, ok := s.authenticateSession(r); ok {
		uv := toUserView(u)
		writeJSON(w, http.StatusOK, authMeResponse{Authenticated: true, User: &uv})
		return
	}

	pending, err := s.setupPending(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authMeResponse{Authenticated: false, SetupRequired: pending})
}

// setupPending checks whether the bootstrap admin (id=1, seeded by
// migration 0005) still has the sentinel password. Phase A only ever has
// this one user, so there's nothing to loop over.
func (s *Server) setupPending(r *http.Request) (bool, error) {
	return s.store.IsSetupPending(r.Context(), 1)
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAuthSetup is public: there's no valid session to require yet.
// It's safe regardless, because store.CompleteSetup only succeeds once
// (its WHERE clause requires the sentinel hash still being in place) --
// after the first successful call, every subsequent call 404s.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}

	pending, err := s.setupPending(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !pending {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	now := s.now()
	if err := s.store.CompleteSetup(r.Context(), 1, req.Username, hash, now); err != nil {
		writeStoreError(w, err)
		return
	}

	s.startSession(w, r, 1)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ip := clientIP(r)
	if !s.loginLimiter.Allow(req.Username, ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}

	u, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		// Burn the same argon2id cost as a real check and record the
		// failure under the attempted username, so a nonexistent user
		// behaves identically to a wrong password (timing + rate limit).
		auth.VerifyDummyPassword(req.Password)
		s.loginLimiter.RecordFailure(req.Username, ip)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	ok, verr := auth.VerifyPassword(u.PasswordHash, req.Password)
	if verr != nil || !ok || u.IsDisabled {
		s.loginLimiter.RecordFailure(req.Username, ip)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	if auth.NeedsRehash(u.PasswordHash) {
		if newHash, herr := auth.HashPassword(req.Password); herr == nil {
			_ = s.store.UpdateUserPassword(r.Context(), u.ID, newHash, s.now())
		}
	}

	s.loginLimiter.RecordSuccess(req.Username)
	s.startSession(w, r, u.ID)
}

// startSession creates a fresh session (rotating the token, per the
// session-fixation defense in docs/multi-user-design.md: a new random
// token is always issued on login/setup, never reused) and writes the
// user view as the response body.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	now := s.now()
	if _, err := s.store.CreateSession(r.Context(), userID, hash, now, now.Add(sessionExpiry)); err != nil {
		writeStoreError(w, err)
		return
	}

	u, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	setSessionCookie(w, r, s.cookieSecureCfg, raw)
	uv := toUserView(u)
	writeJSON(w, http.StatusOK, authMeResponse{Authenticated: true, User: &uv})
}

type updateMeRequest struct {
	InstagramEmbedsEnabled *bool `json:"instagram_embeds_enabled"`
}

// handleAuthUpdateMe updates the caller's own display settings (currently
// just instagram_embeds_enabled -- see
// docs/adr/0001-third-party-embed-in-feed-content.md). Always scoped to
// userFromContext's ID, which comes from the authenticated session/token,
// never from request input, so there's no cross-user ID to validate here.
func (s *Server) handleAuthUpdateMe(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.InstagramEmbedsEnabled == nil {
		writeError(w, http.StatusBadRequest, "instagram_embeds_enabled is required")
		return
	}

	if err := s.store.SetUserInstagramEmbedsEnabled(r.Context(), u.ID, *req.InstagramEmbedsEnabled, s.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	updated, err := s.store.GetUserByID(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserView(updated))
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if raw, ok := sessionCookieFromRequest(r); ok {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(raw))
	}
	clearSessionCookie(w, r, s.cookieSecureCfg)
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.New) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}

	ok, verr := auth.VerifyPassword(u.PasswordHash, req.Current)
	if verr != nil || !ok {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if err := s.store.UpdateUserPassword(r.Context(), u.ID, hash, s.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	// Log out everywhere, including this request's own session: a changed
	// password should invalidate every existing session per the design
	// doc, and re-login establishes a fresh one.
	if err := s.store.DeleteAllSessionsForUser(r.Context(), u.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	clearSessionCookie(w, r, s.cookieSecureCfg)
	w.WriteHeader(http.StatusNoContent)
}

type createAPITokenRequest struct {
	Label string `json:"label"`
}

type createAPITokenResponse struct {
	Token string         `json:"token"`
	Info  store.APIToken `json:"info"`
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createAPITokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	tok, err := s.store.CreateAPIToken(r.Context(), u.ID, req.Label, hash, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createAPITokenResponse{Token: raw, Info: tok})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tokens, err := s.store.ListAPITokensForUser(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if tokens == nil {
		tokens = []store.APIToken{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleDeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteAPIToken(r.Context(), u.ID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
