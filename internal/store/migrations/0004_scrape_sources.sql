-- フィード非提供サイトの購読機能(方式A: pagewatch)の方式固有設定。
-- feeds/subscriptions にはカラムを足さず、ここに切り出す
-- (docs/feedless-site-subscription.md のアーキテクチャ方針参照)。
CREATE TABLE scrape_sources (
  id          INTEGER PRIMARY KEY,
  feed_id     INTEGER NOT NULL UNIQUE REFERENCES feeds(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL,              -- 'pagewatch'(将来 'selector' 等)
  target_url  TEXT    NOT NULL,              -- 実際に取得する URL(http/https)
  config      TEXT    NOT NULL DEFAULT '{}', -- 方式固有の設定(JSON)
  state       TEXT,                          -- 前回抽出の不透明な状態(JSON)
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
