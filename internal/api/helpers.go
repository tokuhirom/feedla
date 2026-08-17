package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/tokuhirom/feedla/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("api: encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= http.StatusInternalServerError {
		slog.Error("api: request failed", "status", status, "error", msg)
	} else {
		slog.Warn("api: request failed", "status", status, "error", msg)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

// storeErrStatus maps a store error to the HTTP status it should produce:
// store.ErrNotFound becomes 404, store.ErrConflict becomes 409,
// store.ErrLastAdmin becomes 400, everything else a 500.
func storeErrStatus(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, store.ErrLastAdmin):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeStoreError(w http.ResponseWriter, err error) {
	writeError(w, storeErrStatus(err), err.Error())
}

func idPathParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
