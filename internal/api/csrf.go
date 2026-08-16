package api

import (
	"net/http"
	"net/url"
	"strings"
)

// checkOrigin wraps a handler with a CSRF defense based on the Origin
// header: for state-changing requests (anything other than GET/HEAD/
// OPTIONS) sent by a browser, it rejects requests whose Origin doesn't
// match the Host the request was sent to. feedla has no session/auth
// system, so there's no cookie-based ambient authority to protect with
// SameSite — the only thing standing between a malicious page and a
// mutating request is this check.
//
// Requests without an Origin header are allowed through: browsers always
// set Origin on cross-origin and same-origin fetch/XHR/form submissions
// that use these methods, so a missing Origin means the request didn't
// come from a browser page (e.g. curl, the Fastladder-compatible clients
// this API targets) and therefore isn't a CSRF risk under this app's
// no-auth threat model.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		originURL, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(originURL.Host, r.Host) {
			writeError(w, http.StatusForbidden, "cross-origin request rejected")
			return
		}

		next.ServeHTTP(w, r)
	})
}
