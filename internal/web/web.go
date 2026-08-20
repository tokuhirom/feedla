// Package web serves the built Preact/Vite SPA (see /web) that feedla
// embeds into its single binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// frame-src allows two destinations: Instagram's own single-post embed page
// -- see docs/adr/0001-third-party-embed-in-feed-content.md and
// internal/crawler/instagram_embed.go, which only ever emits an <iframe src>
// pointing there -- and 'self', for GET /api/v1/scrape_sources/inspect/view
// (feedless Phase F2's safe third-party-page preview, §10.3/§10.7 of
// docs/feedless-site-subscription-selector.md). The latter is safe to embed
// under the app's own origin because that response carries its own,
// separate, far stricter CSP (default-src 'none' etc., set in
// internal/api's handleInspectView) that overrides this one for its own
// document -- 'self' here only lets the *iframe navigation itself* happen.
const csp = "default-src 'self'; img-src 'self' https: data:; script-src 'self'; frame-src https://www.instagram.com 'self'"

// Assets returns the embedded SPA build, rooted at dist/ so callers see
// index.html, assets/, etc. directly instead of dist/index.html.
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler serves the SPA: real static paths (JS/CSS/images) as-is, and
// falls back to index.html for any other GET path. The client has no
// router yet, so today the fallback only matters for "/", but it's cheap
// insurance for direct navigation/reload once one is added.
func Handler() (http.Handler, error) {
	sub, err := Assets()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "."
		}
		if info, err := fs.Stat(sub, path); err != nil || info.IsDir() {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
