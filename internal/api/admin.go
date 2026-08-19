package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"

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

// backupFileInfo describes a single backup snapshot for the admin
// backup-status view (both local files and remote objects render as this).
type backupFileInfo struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	// ModifiedAt is a Unix timestamp (seconds), matching the rest of the
	// API's *_at fields (see internal/store/types.go).
	ModifiedAt int64 `json:"modified_at"`
}

type adminBackupStatusResponse struct {
	LocalEnabled  bool             `json:"local_enabled"`
	LocalDir      string           `json:"local_dir,omitempty"`
	LocalFiles    []backupFileInfo `json:"local_files"`
	RemoteEnabled bool             `json:"remote_enabled"`
	RemoteFiles   []backupFileInfo `json:"remote_files"`
}

// handleAdminBackupStatus reports which local (FR_BACKUP_DIR) and remote
// (FR_BACKUP_REMOTE_*) backup snapshots currently exist, so an admin can
// confirm backups are actually being taken without shelling into the host.
func (s *Server) handleAdminBackupStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}

	resp := adminBackupStatusResponse{
		LocalEnabled:  s.backupDir != "",
		LocalDir:      s.backupDir,
		LocalFiles:    []backupFileInfo{},
		RemoteEnabled: s.backupRemote != nil,
		RemoteFiles:   []backupFileInfo{},
	}

	if s.backupDir != "" {
		files, err := localBackupFiles(s.backupDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list local backups")
			return
		}
		resp.LocalFiles = files
	}

	if s.backupRemote != nil {
		objs, err := s.backupRemote.List(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to list remote backups")
			return
		}
		for _, o := range objs {
			resp.RemoteFiles = append(resp.RemoteFiles, backupFileInfo{
				Name:       o.Key,
				SizeBytes:  o.Size,
				ModifiedAt: o.LastModified.Unix(),
			})
		}
		sort.Slice(resp.RemoteFiles, func(i, j int) bool { return resp.RemoteFiles[i].Name > resp.RemoteFiles[j].Name })
	}

	writeJSON(w, http.StatusOK, resp)
}

// localBackupFiles lists feedla-YYYYMMDD.{db,opml} snapshots under dir,
// sorted by name descending (most recent first). A missing dir (backups
// never taken yet) isn't an error -- it just reports no files.
func localBackupFiles(dir string) ([]backupFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []backupFileInfo{}, nil
		}
		return nil, err
	}

	files := []backupFileInfo{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "feedla-") || (!strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".opml")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, backupFileInfo{Name: name, SizeBytes: info.Size(), ModifiedAt: info.ModTime().Unix()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name > files[j].Name })
	return files, nil
}
