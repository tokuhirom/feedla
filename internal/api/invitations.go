package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/store"
)

// invitationExpiry is the default invitation lifetime from
// docs/multi-user-design.md's 招待トークン制 section (既定 72 時間).
const invitationExpiry = 72 * time.Hour

type adminInvitationResponse struct {
	store.Invitation
	// Token is the raw invitation token, returned only here (like
	// createAPITokenResponse's Token field) -- the DB keeps just its hash,
	// so this response is the admin's one chance to copy the invite link.
	Token string `json:"token"`
}

func (s *Server) handleAdminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	u, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	now := s.now()
	inv, err := s.store.CreateInvitation(r.Context(), u.ID, hash, now.Add(invitationExpiry), now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminInvitationResponse{Invitation: inv, Token: raw})
}

func (s *Server) handleAdminListInvitations(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	invs, err := s.store.ListInvitations(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invs})
}

type invitationTokenRequest struct {
	Token string `json:"token"`
}

// handleInvitationStatus is public (see publicPaths in auth_middleware.go):
// it reports whether a token is still redeemable, so the accept screen can
// show an "expired/used" message before asking for a username/password.
// The token lives in the request body rather than the URL path because
// publicPaths matches on exact "METHOD path" pairs and can't allowlist a
// path containing a variable token.
func (s *Server) handleInvitationStatus(w http.ResponseWriter, r *http.Request) {
	var req invitationTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if err := s.store.CheckInvitation(r.Context(), auth.HashToken(req.Token), s.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"valid": true})
}

type acceptInvitationRequest struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAcceptInvitation is public: there's no session yet at this point.
// It creates the account, consumes the token (store.AcceptInvitation
// refuses reuse even under a race), and logs the new user in immediately,
// the same way setup/login do.
func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req acceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	u, err := s.store.AcceptInvitation(r.Context(), auth.HashToken(req.Token), req.Username, hash, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.startSession(w, r, u.ID)
}
