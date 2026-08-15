-- 「過去24時間・未読・全フィード横断」(サイドバーのTodayグループ)の
-- COUNT/SELECTをフルスキャンさせないための部分インデックス。
CREATE INDEX idx_entries_unread_pub ON entries(published_at DESC) WHERE read_at IS NULL;
