-- 本文抽出(internal/fulltext)の前処理として、同一フィードの記事ページ間で
-- 繰り返し現れる DOM サブツリー(サイト共通のナビ・ヘッダ・フッタ)を除去する
-- ための学習状態。feed 単位の不透明な JSON で、selector 方式(scrape_sources)
-- と feed_fulltext の両経路が同じ行を共有する。
--
-- scrape_sources.state に相乗りさせないのは、あちらが方式固有の state で
-- バージョン不一致時に再同期へ入る仕組みを持つため。こちらは失われても
-- 数クロールで学習し直せるだけの補助データで、行が無い/壊れている場合は
-- 単に除去なしで抽出する。
CREATE TABLE feed_boilerplate (
  feed_id    INTEGER PRIMARY KEY REFERENCES feeds(id) ON DELETE CASCADE,
  state      TEXT    NOT NULL,
  updated_at INTEGER NOT NULL
);
