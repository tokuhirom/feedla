package store

// Folder groups subscriptions, mirroring LDR's folder concept.
type Folder struct {
	ID        int64
	Name      string
	SortOrder int64
}

// Feed is the crawler-owned, objective view of a subscribed feed.
type Feed struct {
	ID               int64
	FeedURL          string
	SiteURL          string
	Title            string
	Description      string
	FaviconURL       string
	ETag             *string
	LastModified     *string
	BodyHash         []byte
	FetchIntervalSec int64
	NextFetchAt      int64
	LastFetchedAt    *int64
	LastSuccessAt    *int64
	LastStatus       *int64
	ErrorCount       int64
	LastError        *string
	CreatedAt        int64
	UpdatedAt        int64
}

// EntryInput is one article as extracted from a feed, ready to be written by
// the crawler. published_at/read_at are only set on first insert; a later
// fetch that sees the same (feed_id, guid) never touches them, so marking an
// entry read never gets clobbered by a re-fetch and a re-published date never
// moves an already-seen entry's position.
type EntryInput struct {
	GUID        string
	URL         string
	Title       string
	Author      string
	Body        string
	BodyHash    []byte
	PublishedAt int64
	UpdatedAt   int64
}

// Subscription is the user-owned view of a Feed (folder, rating, ...).
type Subscription struct {
	FeedID      int64
	FolderID    *int64
	Title       string
	Rating      int64
	UnreadCount int64
	SortOrder   int64
	CreatedAt   int64
}
