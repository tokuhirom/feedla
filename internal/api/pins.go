package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	pins, err := s.store.ListPins(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if pins == nil {
		pins = []store.Pin{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": pins})
}

type addPinRequest struct {
	EntryID int64 `json:"entry_id"`
}

func (s *Server) handleAddPin(w http.ResponseWriter, r *http.Request) {
	var req addPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.EntryID == 0 {
		writeError(w, http.StatusBadRequest, "entry_id is required")
		return
	}

	u, _ := userFromContext(r.Context())
	pinCount, err := s.store.CountPins(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !checkCountQuota(w, pinCount, s.quota.MaxPins, "pins") {
		return
	}

	if err := s.store.AddPin(r.Context(), u.ID, req.EntryID, time.Now()); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry_id": req.EntryID})
}

func (s *Server) handleRemovePin(w http.ResponseWriter, r *http.Request) {
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	u, _ := userFromContext(r.Context())
	if err := s.store.RemovePin(r.Context(), u.ID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
