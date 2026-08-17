package api

import (
	"encoding/json"
	"net/http"

	"github.com/tokuhirom/feedla/internal/auth"
	"github.com/tokuhirom/feedla/internal/store"
)

// requireAdmin returns the authenticated user, writing 403 (not 404 --
// unlike per-resource IDOR checks, the existence of /api/v1/admin/* isn't
// a secret worth hiding) and false if they're not an admin.
// authMiddleware guarantees an authenticated user for every non-public
// route by the time a handler runs, so the !ok case here is unreachable in
// practice; it's handled anyway rather than assumed away.
func requireAdmin(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return store.User{}, false
	}
	if !u.IsAdmin {
		writeError(w, http.StatusForbidden, "admin only")
		return store.User{}, false
	}
	return u, true
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if users == nil {
		users = []store.User{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type adminCreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

// handleAdminCreateUser creates an account directly with admin-chosen
// credentials. docs/multi-user-design.md also describes an invitation-
// token flow as an alternative; this direct-create path is the "admin
// creates the account" option from the same table and is the only one
// implemented so far.
func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}

	var req adminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
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

	u, err := s.store.CreateUser(r.Context(), req.Username, hash, req.IsAdmin, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

type adminUpdateUserRequest struct {
	IsAdmin    *bool `json:"is_admin"`
	IsDisabled *bool `json:"is_disabled"`
}

// handleAdminPatchUser toggles admin/disabled status. Both store calls
// refuse to strip the last enabled admin (store.ErrLastAdmin, mapped to
// 400), so this can never lock every admin out of the admin API.
func (s *Server) handleAdminPatchUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req adminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.IsAdmin != nil {
		if err := s.store.SetUserAdmin(r.Context(), id, *req.IsAdmin, s.now()); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	if req.IsDisabled != nil {
		if err := s.store.SetUserDisabled(r.Context(), id, *req.IsDisabled, s.now()); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	u, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
