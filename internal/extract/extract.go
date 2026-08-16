// Package extract defines the shared contract that page-scraping methods
// (pagewatch, and future url-pattern/selector based methods) implement to
// synthesize a gofeed.Feed from a fetched HTML page. Implementations must
// not import internal/store or internal/crawler; internal/crawler imports
// this package, never the other way around.
package extract

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mmcdole/gofeed"
)

// Kind identifies an extraction method, stored as scrape_sources.kind.
type Kind string

const KindPageWatch Kind = "pagewatch"

// Input is what a caller (the crawler) hands to an Extractor: the page just
// fetched, plus the opaque State returned by the previous Extract call for
// this source (nil on first run).
type Input struct {
	URL         string
	HTML        []byte // UTF-8, already charset-decoded
	ContentType string
	Now         time.Time
	Config      json.RawMessage // scrape_sources.config
	State       json.RawMessage // scrape_sources.state; nil/empty on first run
}

// Result is what an Extractor returns: a synthesized feed (Feed.Items may be
// empty when nothing changed) plus the opaque state to persist for next
// time. A nil State means "leave the stored state as-is", which callers use
// to avoid rewriting unchanged state on every poll.
type Result struct {
	Feed  *gofeed.Feed
	State json.RawMessage
}

// Extractor turns a fetched page into a Result.
type Extractor interface {
	Extract(ctx context.Context, in Input) (*Result, error)
}
