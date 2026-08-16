-- Phase A 認証基盤: users / sessions / api_tokens。
-- 既存テーブル(feeds/entries/subscriptions 等)は無変更。
-- マルチユーザー化 Phase B/C の user_id 化は別マイグレーションで扱う。

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  is_disabled   INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

-- '!locked!' は argon2id の PHC 文字列として絶対に一致しない番兵値。
-- このパスワードのユーザーが存在する間は、初回セットアップ画面でのみ
-- パスワードを設定できる(§CompleteSetup)。
INSERT INTO users (id, username, password_hash, is_admin, is_disabled, created_at, updated_at)
VALUES (1, 'admin', '!locked!', 1, 0, strftime('%s', 'now'), strftime('%s', 'now'));

CREATE TABLE sessions (
  id            INTEGER PRIMARY KEY,
  token_hash    BLOB NOT NULL UNIQUE,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
  id            INTEGER PRIMARY KEY,
  token_hash    BLOB NOT NULL UNIQUE,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label         TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);
