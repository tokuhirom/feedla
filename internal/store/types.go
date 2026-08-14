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
	FetchIntervalSec int64
	NextFetchAt      int64
	CreatedAt        int64
	UpdatedAt        int64
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
