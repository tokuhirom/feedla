package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tokuhirom/feedla/internal/store"
)

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	folders, err := s.store.ListFolders(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if folders == nil {
		folders = []store.Folder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

type createFolderRequest struct {
	Name string `json:"name"`
}

// handleCreateFolder creates a folder, or returns the existing one if a
// folder with this name already exists (store.GetOrCreateFolder is
// get-or-create, matching how the UI lets a user "create" a folder they
// may have already made).
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req createFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	u, _ := userFromContext(r.Context())
	id, err := s.store.GetOrCreateFolder(r.Context(), u.ID, name)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, store.Folder{ID: id, Name: name})
}
