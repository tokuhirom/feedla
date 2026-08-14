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
