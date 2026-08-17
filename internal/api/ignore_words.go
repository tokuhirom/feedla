package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

func (s *Server) handleListIgnoreWords(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	words, err := s.store.ListIgnoreWords(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if words == nil {
		words = []store.IgnoreWord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ignore_words": words})
}

type addIgnoreWordRequest struct {
	Word string `json:"word"`
}

func (s *Server) handleAddIgnoreWord(w http.ResponseWriter, r *http.Request) {
	var req addIgnoreWordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Word == "" {
		writeError(w, http.StatusBadRequest, "word is required")
		return
	}

	u, _ := userFromContext(r.Context())
	wordCount, err := s.store.CountIgnoreWords(r.Context(), u.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !checkCountQuota(w, wordCount, s.quota.MaxIgnoreWords, "ignore words") {
		return
	}

	if err := s.store.AddIgnoreWord(r.Context(), u.ID, req.Word, time.Now()); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"word": req.Word})
}

func (s *Server) handleRemoveIgnoreWord(w http.ResponseWriter, r *http.Request) {
	id, err := idPathParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	u, _ := userFromContext(r.Context())
	if err := s.store.RemoveIgnoreWord(r.Context(), u.ID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
