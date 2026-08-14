package api

import (
	"net/http"

	"github.com/tokuhirom/feedla/internal/feed"
)

// maxOPMLUploadBytes bounds how large an imported OPML document may be, so
// a malformed or malicious upload can't exhaust memory.
const maxOPMLUploadBytes = 10 << 20 // 10 MiB

func (s *Server) handleExportOPML(w http.ResponseWriter, r *http.Request) {
	out, err := feed.ExportOPML(r.Context(), s.store)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="feedla-subscriptions.opml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func (s *Server) handleImportOPML(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOPMLUploadBytes)
	n, err := feed.ImportOPML(r.Context(), s.store, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": n})
}
