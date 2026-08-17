package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/tokuhirom/feedla/internal/auth"
)

// rateLimitMiddleware enforces FR_QUOTA_API_PER_MINUTE, the coarse
// API-wide abuse backstop from docs/multi-user-design.md's リソース制限・
// abuse 対策 section. It runs inside authMiddleware, so an authenticated
// user is normally in context; unauthenticated requests (login, setup,
// invitations) are left to their own protections (LoginLimiter, low
// expected volume) and pass through untouched.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := userFromContext(r.Context()); ok {
			if !s.apiLimiter.Allow(strconv.FormatInt(u.ID, 10)) {
				writeError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// checkActionQuota enforces one of the FR_QUOTA_*_PER_HOUR action limits
// (feed add, manual refresh, pagewatch preview), writing a 429 and
// returning false if userID has exceeded it. limiter is one of
// s.feedAddLimiter/s.refreshLimiter/s.previewLimiter, each already scoped
// to its own action, so the key only needs to distinguish users.
func checkActionQuota(w http.ResponseWriter, limiter *auth.ActionLimiter, userID int64, action string) bool {
	if !limiter.Allow(strconv.FormatInt(userID, 10)) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("quota exceeded: too many %s requests, try again later", action))
		return false
	}
	return true
}

// checkCountQuota enforces one of the FR_QUOTA_MAX_* resource-count limits.
// max <= 0 disables the check (matching Options{} in tests that don't care
// about quotas). Writes a 400 and returns false if count has already
// reached max.
func checkCountQuota(w http.ResponseWriter, count, max int, resource string) bool {
	if max > 0 && count >= max {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("quota exceeded: max %d %s", max, resource))
		return false
	}
	return true
}
