package api

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipMiddleware compresses response bodies with gzip when the client sends
// Accept-Encoding: gzip. The API never sets Content-Length itself (writeJSON
// streams straight to json.Encoder, opml.go streams a []byte) and never
// serves Range requests, so wrapping every response here -- including error
// bodies -- is safe, unlike doing the same for internal/web's static file
// server (http.FileServerFS honors Range/ETag/Content-Length, which gzip
// would break).
//
// This exists because a large JSON response (e.g. GET /api/v1/subscriptions
// on an account with 1000+ feeds) can take over a second to transfer
// uncompressed even though the server itself answers in tens of
// milliseconds -- gzip cuts JSON to roughly a tenth of its size.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set regardless of whether this request negotiates gzip: a shared
		// cache in front of feedla (reverse proxy/CDN) must know the
		// response varies on this header, or it may cache an identity
		// response and serve it to a client that does support gzip.
		w.Header().Add("Vary", "Accept-Encoding")

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzip.NewWriter(w)
		defer func() { _ = gz.Close() }()

		w.Header().Set("Content-Encoding", "gzip")
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gz.Write(b)
}

// Flush satisfies http.Flusher by flushing gz's own buffer before the
// underlying connection -- without this override, the embedded
// http.ResponseWriter's Flush would promote through unchanged and flush the
// socket while compressed bytes are still sitting in gz, silently stalling
// any future streaming response (e.g. SSE) wrapped by this middleware.
func (w gzipResponseWriter) Flush() {
	_ = w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
