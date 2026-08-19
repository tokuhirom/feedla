-- entry本文抽出(要約止まりのフィードに対し、entryのリンク先ページから
-- 本文を抽出して差し替える機能)の有効化フラグ。feedless(scrape_sources)
-- とは無関係の別機構であり、行の有無がそのまま有効/無効を表す
-- (feed_idごとに高々1行、config的なチューニング項目は現時点で不要)。
CREATE TABLE feed_fulltext (
  feed_id    INTEGER PRIMARY KEY REFERENCES feeds(id) ON DELETE CASCADE,
  created_by INTEGER NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL
);
