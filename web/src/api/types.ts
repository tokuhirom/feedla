// Mirrors the JSON shapes produced by internal/store/types.go and
// internal/feed/discover.go. Field names stay snake_case to match the
// wire format 1:1 -- no conversion layer.

export interface SubscriptionView {
  feed_id: number
  feed_url: string
  site_url?: string
  title: string
  folder_id?: number
  rating: number
  unread_count: number
  last_status?: number
  error_count: number
  last_error?: string
  last_fetched_at?: number
  next_fetch_at: number
  last_entry_at?: number
}

export interface Entry {
  id: number
  feed_id: number
  guid: string
  url: string
  title: string
  author?: string
  body: string
  published_at: number
  updated_at: number
  fetched_at: number
  read_at?: number
  pinned: boolean
}

export interface Folder {
  id: number
  name: string
  sort_order: number
}

export interface Candidate {
  title: string
  feed_url: string
}

export interface Pin {
  entry_id: number
  url: string
  title: string
  created_at: number
}

export interface IgnoreWord {
  id: number
  word: string
  created_at: number
}

export interface InternalErrorEntry {
  feed_id: number
  feed_url: string
  error: string
  at: number
}

export interface Stats {
  feeds_total: number
  feeds_erroring: number
  entries_unread: number
  queue_depth: number
  db_size_bytes: number
  erroring_feeds?: SubscriptionView[]
  internal_errors?: InternalErrorEntry[]
}
