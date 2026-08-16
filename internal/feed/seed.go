package feed

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"

	"github.com/tokuhirom/feedla/internal/store"
)

// seedOPML is feedla's default subscription list, applied by SeedIfEmpty on
// a fresh install so a new deployment isn't a blank slate. Edit
// seed.opml and rebuild to change what a new instance starts with.
//
//go:embed seed.opml
var seedOPML []byte

// SeedIfEmpty imports the embedded default OPML the first time feedla runs
// against a database with no feeds yet, so a fresh volume/deployment starts
// pre-subscribed instead of empty. It's a no-op once any feed exists.
// Seeding always targets the bootstrap admin (id=1, unconditionally seeded
// by migration 0005) -- this only ever runs against a genuinely fresh DB,
// before any other user could exist.
func SeedIfEmpty(ctx context.Context, st *store.Store) (int, error) {
	const bootstrapAdminID = 1

	feeds, err := st.ListFeeds(ctx)
	if err != nil {
		return 0, fmt.Errorf("feed: seed: list feeds: %w", err)
	}
	if len(feeds) > 0 {
		return 0, nil
	}

	n, err := ImportOPML(ctx, st, bootstrapAdminID, bytes.NewReader(seedOPML))
	if err != nil {
		return 0, fmt.Errorf("feed: seed: %w", err)
	}
	return n, nil
}
