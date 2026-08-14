-- 無視ワード。タイトル/本文に含まれる記事を未読一覧・未読カウントから除外する
CREATE TABLE ignore_words (
  id         INTEGER PRIMARY KEY,
  word       TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

ALTER TABLE entries ADD COLUMN ignored INTEGER NOT NULL DEFAULT 0;
