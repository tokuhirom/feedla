package feed

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// defaultFetchIntervalSec is the initial crawl interval assigned to feeds
// imported from OPML, before adaptive scheduling kicks in.
const defaultFetchIntervalSec = 1800

// ImportOPML parses an OPML document and upserts every feed/folder/
// subscription it contains on userID's behalf. It returns the number of
// feeds imported. maxFeeds enforces FR_QUOTA_OPML_MAX_FEEDS -- checked
// against the parsed document up front, before anything is written, so an
// over-limit import fails atomically instead of partially applying; a
// non-positive maxFeeds disables the check.
func ImportOPML(ctx context.Context, st *store.Store, userID int64, r io.Reader, maxFeeds int) (int, error) {
	feeds, err := ParseOPML(r)
	if err != nil {
		return 0, err
	}
	if maxFeeds > 0 && len(feeds) > maxFeeds {
		return 0, fmt.Errorf("feed: import: %d feeds exceeds the %d-feed OPML import limit", len(feeds), maxFeeds)
	}

	now := time.Now()
	folderIDs := make(map[string]int64)

	imported := 0
	for _, f := range feeds {
		if strings.HasPrefix(f.FeedURL, crawler.ScrapePrefix) {
			// ExportOPML never emits these (§12 #7), so a "pagewatch:"
			// xmlUrl here can only come from a hand-edited or foreign file.
			// Importing it verbatim would create a feeds row with no
			// matching scrape_sources row, which crawlOne can't fetch.
			continue
		}

		var folderID *int64
		if f.FolderName != "" {
			id, ok := folderIDs[f.FolderName]
			if !ok {
				id, err = st.GetOrCreateFolder(ctx, userID, f.FolderName)
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

		if err := st.UpsertSubscription(ctx, userID, feedID, folderID, f.Title, now); err != nil {
			return imported, fmt.Errorf("feed: import %q: %w", f.FeedURL, err)
		}

		imported++
	}
	return imported, nil
}
