package api

import "net/http"

// sessionMaxAge is the session cookie's Max-Age and the absolute session
// lifetime (docs/multi-user-design.md: 90 days). There's also a 30-day
// idle timeout enforced in auth_middleware.go by checking last_seen_at
// against sessionIdleTimeout independently of this cookie lifetime.
const sessionMaxAgeSeconds = 90 * 24 * 60 * 60

// cookieName returns the session cookie name: the "__Host-" prefix
// requires Secure, Path=/, and no Domain attribute, all of which we always
// set, but browsers only honor the prefix on HTTPS origins -- so it's only
// used when secure is true.
func cookieName(secure bool) string {
	if secure {
		return "__Host-session"
	}
	return "session"
}

// cookieSecure resolves the FR_COOKIE_SECURE config value ("auto" (or
// unset)/"true"/"false") against the current request. "auto" only trusts
// r.TLS (a direct TLS termination in this process); it never trusts
// X-Forwarded-Proto, since that requires explicitly configuring which
// proxies are trustworthy, which feedla doesn't yet support.
func cookieSecure(cookieSecureCfg string, r *http.Request) bool {
	switch cookieSecureCfg {
	case "true":
		return true
	case "false":
		return false
	default: // "auto", ""
		return r.TLS != nil
	}
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, cookieSecureCfg, rawToken string) {
	secure := cookieSecure(cookieSecureCfg, r)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(secure),
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAgeSeconds,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, cookieSecureCfg string) {
	secure := cookieSecure(cookieSecureCfg, r)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(secure),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// sessionCookieFromRequest reads whichever of the two cookie names is
// present -- a deployment may have switched FR_COOKIE_SECURE (and thus the
// cookie name) since a client's last login, so both are checked.
func sessionCookieFromRequest(r *http.Request) (string, bool) {
	if c, err := r.Cookie("__Host-session"); err == nil {
		return c.Value, true
	}
	if c, err := r.Cookie("session"); err == nil {
		return c.Value, true
	}
	return "", false
}
