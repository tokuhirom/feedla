package feed

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/tokuhirom/feedla/internal/store"
)

// defaultFetchIntervalSec is the initial crawl interval assigned to feeds
// imported from OPML, before adaptive scheduling kicks in.
const defaultFetchIntervalSec = 1800

// ImportOPML parses an OPML document and upserts every feed/folder/
// subscription it contains. It returns the number of feeds imported.
func ImportOPML(ctx context.Context, st *store.Store, r io.Reader) (int, error) {
	feeds, err := ParseOPML(r)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	folderIDs := make(map[string]int64)

	imported := 0
	for _, f := range feeds {
		var folderID *int64
		if f.FolderName != "" {
			id, ok := folderIDs[f.FolderName]
			if !ok {
				id, err = st.GetOrCreateFolder(ctx, f.FolderName)
				if err != nil {
					return imported, fmt.Errorf("feed: import %q: %w", f.FeedURL, err)
				}
				folderIDs[f.FolderName] = id
			}
			folderID = &id
		}

		feedID, err := st.UpsertFeed(ctx, f.FeedURL, f.SiteURL, f.Title, defaultFetchIntervalSec, now)
		if err != nil {
			return imported, fmt.Errorf("feed: import %q: %w", f.FeedURL, err)
		}

		if err := st.UpsertSubscription(ctx, feedID, folderID, f.Title, now); err != nil {
			return imported, fmt.Errorf("feed: import %q: %w", f.FeedURL, err)
		}

		imported++
	}
	return imported, nil
}
