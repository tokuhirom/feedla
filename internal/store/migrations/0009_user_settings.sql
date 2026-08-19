-- ユーザー単位の表示設定。まずInstagram埋め込み表示のON/OFFのみ
-- (docs/adr/0001-third-party-embed-in-feed-content.md)。端末をまたいで
-- 同じアカウントなら設定を引き継げるよう、ブラウザのlocalStorageではなく
-- サーバー側(users テーブル)に持つ。
ALTER TABLE users ADD COLUMN instagram_embeds_enabled INTEGER NOT NULL DEFAULT 0;
