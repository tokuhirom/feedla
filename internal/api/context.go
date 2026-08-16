package api

import (
	"context"

	"github.com/tokuhirom/feedla/internal/store"
)

type contextKey int

const userContextKey contextKey = iota

func contextWithUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// userFromContext returns the authenticated user set by authMiddleware.
// Handlers reached through NewHandler's mux always have one (the
// middleware rejects unauthenticated requests with 401 before any handler
// runs), so ok=false here indicates a wiring bug, not a normal 401 path.
func userFromContext(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userContextKey).(store.User)
	return u, ok
}
