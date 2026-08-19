// Mirrors the JSON shapes produced by internal/store/types.go and
// internal/feed/discover.go. Field names stay snake_case to match the
// wire format 1:1 -- no conversion layer.

export type SubscriptionKind = 'feed' | 'pagewatch'

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
  kind: SubscriptionKind
  // Fulltext is true when internal/fulltext extraction is enabled for this
  // feed -- unrelated to kind ("pagewatch" is the feedless/scrape_sources
  // axis; this augments a real feed's entry bodies instead).
  fulltext: boolean
}

// Mirrors internal/extract/pagewatch.Config.
export interface PagewatchConfig {
  ignore_patterns?: string[]
  min_change_chars?: number
  watch_mode?: 'additions' | 'changes'
  include_full_body?: boolean
  guid_mode?: 'content' | 'revision'
  scope_selector?: string
}

// Mirrors internal/api's scrapeSourceView (State is intentionally excluded
// server-side -- it's disposable crawl bookkeeping, not config).
export interface ScrapeSource {
  id: number
  feed_id: number
  kind: string
  target_url: string
  config: PagewatchConfig
  created_at: number
  updated_at: number
}

// Mirrors internal/extract/pagewatch.PreviewBlock.
export interface PreviewBlock {
  text: string
  masked: boolean
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
  // fulltext marks the synthetic "同じフィードを本文抽出ありで購読する"
  // entry the server appends to the real discovered candidates -- see
  // internal/api's candidateView.
  fulltext: boolean
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

export interface HealthStatus {
  status: string
  version: string
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

export interface AdminUser {
  id: number
  username: string
  is_admin: boolean
  is_disabled: boolean
  created_at: number
  updated_at: number
}

export interface Invitation {
  id: number
  created_by: number
  expires_at: number
  used_by: number | null
  used_at: number | null
  created_at: number
}

export interface BackupFile {
  name: string
  size_bytes: number
  modified_at: number
}

export interface AdminBackupStatus {
  local_enabled: boolean
  local_dir?: string
  local_files: BackupFile[]
  remote_enabled: boolean
  remote_files: BackupFile[]
}
