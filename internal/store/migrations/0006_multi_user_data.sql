-- Phase B マルチユーザー化: データモデルの user_id 分離。
-- 0005 で作った users(1人だけ: id=1) を基準に、購読・既読・pin・フォルダ・
-- 無視ワードを user_id で分離する。fan-out-on-write 方式
-- (docs/multi-user-design.md 「既読状態の持ち方」参照)を採用。SQLite は
-- 複合主キーへの ALTER ができないため、対象テーブルは
-- create → copy → drop → rename で作り直す。
--
-- 前提: internal/store/migrate.go の applyMigration が
-- PRAGMA foreign_keys=OFF の状態(トランザクション外で切替済み)で
-- このスクリプトを実行する。ON のまま親テーブル(folders 等)を DROP すると、
-- 既存の ON DELETE SET NULL/CASCADE が「親テーブル DROP = 全行 DELETE」として
-- 発火し、子テーブル(subscriptions.folder_id 等)を巻き込んで壊す
-- (実機で再現・確認済み。internal/store/migrate_internal_test.go 参照)。

-- 1. user_entry_state: ユーザーごとの既読・無視状態(fan-out-on-write)。
CREATE TABLE user_entry_state (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entry_id     INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  feed_id      INTEGER NOT NULL,        -- entries.feed_id の非正規化コピー(索引用)
  published_at INTEGER NOT NULL,        -- entries.published_at の非正規化コピー(同上)
  read_at      INTEGER,                 -- NULL = 未読
  ignored      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, entry_id)
) WITHOUT ROWID;

CREATE INDEX idx_ues_unread
  ON user_entry_state(user_id, feed_id, published_at)
  WHERE read_at IS NULL;

CREATE INDEX idx_ues_unread_pub
  ON user_entry_state(user_id, published_at DESC)
  WHERE read_at IS NULL;

CREATE INDEX idx_ues_entry ON user_entry_state(entry_id);

-- 2. folders: user_id を追加し、UNIQUE を (user_id, name) に。
--    id は既存値を維持する(subscriptions.folder_id との対応を壊さないため)。
CREATE TABLE folders_new (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  UNIQUE(user_id, name)
);
INSERT INTO folders_new (id, user_id, name, sort_order)
SELECT id, 1, name, sort_order FROM folders;
DROP TABLE folders;
ALTER TABLE folders_new RENAME TO folders;

-- 3. subscriptions: (user_id, feed_id) の複合主キーへ。
CREATE TABLE subscriptions_new (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  folder_id    INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  title        TEXT,
  rating       INTEGER NOT NULL DEFAULT 0,
  is_public    INTEGER NOT NULL DEFAULT 0,
  unread_count INTEGER NOT NULL DEFAULT 0,
  sort_order   INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (user_id, feed_id)
) WITHOUT ROWID;
INSERT INTO subscriptions_new (user_id, feed_id, folder_id, title, rating,
                                is_public, unread_count, sort_order, created_at)
SELECT 1, feed_id, folder_id, title, rating, is_public, unread_count, sort_order, created_at
FROM subscriptions;
DROP TABLE subscriptions;
ALTER TABLE subscriptions_new RENAME TO subscriptions;
CREATE INDEX idx_subscriptions_feed ON subscriptions(feed_id);

-- 4. pins: (user_id, entry_id) の複合主キーへ。
CREATE TABLE pins_new (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entry_id   INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  title      TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, entry_id)
) WITHOUT ROWID;
INSERT INTO pins_new (user_id, entry_id, url, title, created_at)
SELECT 1, entry_id, url, title, created_at FROM pins;
DROP TABLE pins;
ALTER TABLE pins_new RENAME TO pins;
CREATE INDEX idx_pins_entry ON pins(entry_id);

-- 5. ignore_words: user_id を追加し、UNIQUE を (user_id, word) に。id は維持。
CREATE TABLE ignore_words_new (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  word       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, word)
);
INSERT INTO ignore_words_new (id, user_id, word, created_at)
SELECT id, 1, word, created_at FROM ignore_words;
DROP TABLE ignore_words;
ALTER TABLE ignore_words_new RENAME TO ignore_words;

-- 6. 既存の entries.read_at / ignored を user_entry_state へバックフィル。
--    購読(=subscriptions に行がある)フィードの記事のみ対象。
INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, read_at, ignored)
SELECT 1, e.id, e.feed_id, e.published_at, e.read_at, e.ignored
FROM entries e
WHERE EXISTS (SELECT 1 FROM subscriptions s WHERE s.feed_id = e.feed_id AND s.user_id = 1);

-- 7. entries から read_at/ignored と、それに依存する部分インデックスを除去。
--    先に依存インデックスを DROP しないと DROP COLUMN が失敗する。
--    entries.id(=entries_fts の content_rowid)は変わらず、FTS 同期トリガ
--    (entries_ai/ad/au)も title/body しか参照しないため、
--    entries_fts の rebuild は不要(実機検証済み)。
DROP INDEX idx_entries_feed_unread;
DROP INDEX idx_entries_unread_pub;
DROP INDEX idx_entries_gc;
ALTER TABLE entries DROP COLUMN read_at;
ALTER TABLE entries DROP COLUMN ignored;

-- 8. scrape_sources.created_by を追加。
--    NOTE: ALTER TABLE ADD COLUMN は「REFERENCES + 非NULLデフォルト」の
--    組み合わせを禁止している(実機検証で確認)。REFERENCES 句は付けず、
--    アプリ側で users.id との対応を守る非強制の論理FKとして扱う
--    (users は物理削除ではなく is_disabled による無効化が基本のため実害は小さい)。
ALTER TABLE scrape_sources ADD COLUMN created_by INTEGER NOT NULL DEFAULT 1;
