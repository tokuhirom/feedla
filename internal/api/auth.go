package api

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"path"
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
	Authenticated bool         `json:"authenticated"`
	SetupRequired bool         `json:"setup_required"`
	User          *userView    `json:"user,omitempty"`
	RestoreHint   *restoreHint `json:"restore_hint,omitempty"`
}

// restoreHint tells the not-yet-set-up SPA why it's looking at the setup
// screen instead of having transparently recovered a prior DB: whether
// local/remote backup restore was even configured, and if so, whether it
// actually found anything to restore from. It's computed live (not cached
// from the internal/restore.IfMissing call at boot) by re-checking the same
// local dir / remote bucket, since a wrong-looking answer is more useful to
// an operator than a stale one -- and it deliberately excludes the actual
// FR_BACKUP_DIR path or bucket/endpoint values, since this response is
// reachable pre-auth.
type restoreHint struct {
	LocalConfigured   bool `json:"local_configured"`
	LocalHasSnapshot  bool `json:"local_has_snapshot"`
	RemoteConfigured  bool `json:"remote_configured"`
	RemoteHasSnapshot bool `json:"remote_has_snapshot"`
	// RemoteError is true when RemoteConfigured but listing the bucket
	// failed (bad endpoint/credentials/network) -- distinct from "configured
	// but genuinely empty" so the operator knows to check FR_BACKUP_REMOTE_*
	// rather than assume there's really nothing to restore.
	RemoteError bool `json:"remote_error"`
	// RestoreSupported reports whether this server can act on
	// POST /api/v1/auth/restore at all (cmd/feedla wires that up; other
	// embedders may not), so the setup screen only offers the restore
	// choice when clicking it can work.
	RestoreSupported bool `json:"restore_supported"`
	// LatestSnapshot is the newest .db snapshot's bare file name
	// (feedla-YYYYMMDD.db) across local and remote, and
	// LatestSnapshotSource says which side it came from ("local" or
	// "remote") -- what POST /api/v1/auth/restore would restore. Empty
	// when no snapshot was found. Deliberately just the file name: no
	// FR_BACKUP_DIR path or bucket/endpoint values (pre-auth response).
	LatestSnapshot       string `json:"latest_snapshot,omitempty"`
	LatestSnapshotSource string `json:"latest_snapshot_source,omitempty"`
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
	resp := authMeResponse{Authenticated: false, SetupRequired: pending}
	if pending {
		hint := s.restoreHintForSetup(r)
		resp.RestoreHint = &hint
	}
	writeJSON(w, http.StatusOK, resp)
}

// restoreHintForSetup is only called when setup is still pending, so it's
// fine to make a live remote List() call here -- the setup screen is seen
// at most a handful of times per instance, unlike a hot polling path.
func (s *Server) restoreHintForSetup(r *http.Request) restoreHint {
	hint := restoreHint{
		LocalConfigured:  s.backupDir != "",
		RemoteConfigured: s.backupRemote != nil,
		RestoreSupported: s.setupRestore != nil,
	}
	// Newest .db snapshot per side; base names embed a sortable YYYYMMDD
	// date, so a plain string comparison picks the more recent one (the
	// same convention internal/restore.Latest uses to decide what an
	// actual restore would fetch).
	var latestLocal, latestRemote string
	if s.backupDir != "" {
		if files, err := localBackupFiles(s.backupDir); err == nil {
			hint.LocalHasSnapshot = len(files) > 0
			// localBackupFiles sorts by name descending.
			for _, f := range files {
				if strings.HasSuffix(f.Name, ".db") {
					latestLocal = f.Name
					break
				}
			}
		}
	}
	if s.backupRemote != nil {
		objs, err := s.backupRemote.List(r.Context())
		if err != nil {
			hint.RemoteError = true
		} else {
			hint.RemoteHasSnapshot = len(objs) > 0
			for _, o := range objs {
				name := path.Base(o.Key)
				if strings.HasSuffix(name, ".db") && name > latestRemote {
					latestRemote = name
				}
			}
		}
	}
	switch {
	case latestRemote > latestLocal:
		hint.LatestSnapshot, hint.LatestSnapshotSource = latestRemote, "remote"
	case latestLocal != "":
		hint.LatestSnapshot, hint.LatestSnapshotSource = latestLocal, "local"
	}
	return hint
}

// handleAuthRestore is the setup screen's "restore from backup instead of
// creating a new admin" choice. Public like handleAuthSetup, and gated the
// same way: it only works while setup is still pending, which is already a
// trust-on-first-use window (anyone reaching the instance could just as
// well complete setup and own it). The wired-in callback stages the newest
// snapshot and schedules an in-process server restart to swap it in, so a
// 202 here means "restarting soon" -- the SPA polls /auth/me until the
// restored instance is back.
func (s *Server) handleAuthRestore(w http.ResponseWriter, r *http.Request) {
	pending, err := s.setupPending(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !pending {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}
	if s.setupRestore == nil {
		writeError(w, http.StatusNotImplemented, "restore is not supported by this server")
		return
	}
	if err := s.setupRestore(r.Context()); err != nil {
		slog.Error("api: setup restore failed", "error", err)
		writeError(w, http.StatusInternalServerError, "restore failed; check server logs")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
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
