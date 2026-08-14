-- フォルダ
CREATE TABLE folders (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  sort_order INTEGER NOT NULL DEFAULT 0
);

-- フィード自体（クローラが管理する客観的情報）
CREATE TABLE feeds (
  id              INTEGER PRIMARY KEY,
  feed_url        TEXT NOT NULL UNIQUE,
  site_url        TEXT,
  title           TEXT NOT NULL DEFAULT '',
  description     TEXT NOT NULL DEFAULT '',
  favicon_url     TEXT,

  -- 条件付き GET 用
  etag            TEXT,
  last_modified   TEXT,
  body_hash       BLOB,            -- 正規化後の本文ハッシュ（304 非対応サーバ向け）

  -- スケジューリング
  fetch_interval_sec INTEGER NOT NULL DEFAULT 1800,
  next_fetch_at   INTEGER NOT NULL,   -- unix time
  last_fetched_at INTEGER,
  last_success_at INTEGER,
  last_status     INTEGER,            -- 直近の HTTP status
  error_count     INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT,

  -- 更新頻度の推定用
  avg_entries_per_day REAL NOT NULL DEFAULT 0,

  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_feeds_next_fetch ON feeds(next_fetch_at) WHERE error_count < 20;

-- 購読（ユーザー側の属性）。シングルユーザーだが feed とは分離しておく
CREATE TABLE subscriptions (
  feed_id      INTEGER PRIMARY KEY REFERENCES feeds(id) ON DELETE CASCADE,
  folder_id    INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  title        TEXT,               -- ユーザーによる上書き
  rating       INTEGER NOT NULL DEFAULT 0,   -- LDR の ★ 相当 (0..5)
  is_public    INTEGER NOT NULL DEFAULT 0,
  unread_count INTEGER NOT NULL DEFAULT 0,   -- キャッシュ
  sort_order   INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL
);

-- 記事
CREATE TABLE entries (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid         TEXT NOT NULL,       -- guid/id、無ければ link、無ければ hash(title+content)
  url          TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  author       TEXT NOT NULL DEFAULT '',
  body         TEXT NOT NULL DEFAULT '',   -- サニタイズ済み HTML
  body_hash    BLOB NOT NULL,
  published_at INTEGER NOT NULL,    -- フィードに無ければ初回取得時刻
  updated_at   INTEGER NOT NULL,
  fetched_at   INTEGER NOT NULL,
  read_at      INTEGER,             -- NULL = 未読
  UNIQUE(feed_id, guid)
);
CREATE INDEX idx_entries_feed_pub    ON entries(feed_id, published_at DESC, id DESC);
CREATE INDEX idx_entries_feed_unread ON entries(feed_id, published_at) WHERE read_at IS NULL;
CREATE INDEX idx_entries_gc          ON entries(fetched_at) WHERE read_at IS NOT NULL;

-- あとで読む
CREATE TABLE pins (
  entry_id   INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  title      TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

-- 全文検索（external content table で二重持ちを回避）
CREATE VIRTUAL TABLE entries_fts USING fts5(
  title, body,
  content='entries', content_rowid='id',
  tokenize="trigram"          -- 日本語対応のため trigram
);

-- entries と entries_fts の同期トリガ
CREATE TRIGGER entries_ai AFTER INSERT ON entries BEGIN
  INSERT INTO entries_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;

CREATE TRIGGER entries_ad AFTER DELETE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
END;

CREATE TRIGGER entries_au AFTER UPDATE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
  INSERT INTO entries_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
