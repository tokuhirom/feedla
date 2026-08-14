package store

// Folder groups subscriptions, mirroring LDR's folder concept.
type Folder struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int64  `json:"sort_order"`
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

// SubscriptionView joins a Subscription with its Feed's display/crawl
// metadata — the shape the subscription-list API actually wants, so
// callers don't have to stitch two queries together themselves.
type SubscriptionView struct {
	FeedID      int64   `json:"feed_id"`
	FeedURL     string  `json:"feed_url"`
	SiteURL     string  `json:"site_url,omitempty"`
	Title       string  `json:"title"`
	FolderID    *int64  `json:"folder_id,omitempty"`
	Rating      int64   `json:"rating"`
	UnreadCount int64   `json:"unread_count"`
	LastStatus  *int64  `json:"last_status,omitempty"`
	ErrorCount  int64   `json:"error_count"`
	LastError   *string `json:"last_error,omitempty"`
}

// Entry is one article as read back from the store.
type Entry struct {
	ID          int64  `json:"id"`
	FeedID      int64  `json:"feed_id"`
	GUID        string `json:"guid"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Author      string `json:"author,omitempty"`
	Body        string `json:"body"`
	PublishedAt int64  `json:"published_at"`
	UpdatedAt   int64  `json:"updated_at"`
	FetchedAt   int64  `json:"fetched_at"`
	ReadAt      *int64 `json:"read_at,omitempty"`
	Pinned      bool   `json:"pinned"`
}

// Pin is a "read later" bookmark on an entry.
type Pin struct {
	EntryID   int64  `json:"entry_id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
}

// EntryCursor is the pagination cursor for ListEntries: the
// (published_at, id) of the last entry seen, matching idx_entries_feed_pub's
// sort order.
type EntryCursor struct {
	PublishedAt int64
	ID          int64
}
