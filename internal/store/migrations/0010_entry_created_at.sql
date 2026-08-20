-- entries.fetched_at は「初回登録時刻」ではなく「最後にそのフィードで
-- 観測した(=再クロールで見つけた)時刻」も兼ねている(UpsertEntries の
-- UPDATE 文が既存 entry を再クロールで観測するたびに更新する。GC が
-- 「まだフィードに存在するか」を判定する用途で使うため)。
-- サイドバーの「Today」グループが本当に見せたいのは「今日 DB に新規登録
-- された entry」なので、初回 INSERT 時のみ確定する created_at を別途持つ。
--
-- created_at は entries に一本だけ持ち、user_entry_state には
-- published_at と同様に非正規化コピーを置く(CountTodayUnread が
-- entries への JOIN なしに完結できるようにするため)。
--
-- 既存行の created_at は不明なので fetched_at で近似バックフィルする
-- (再クロールで更新されていない行は fetched_at == 初回登録時刻と一致する)。
ALTER TABLE entries ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
UPDATE entries SET created_at = fetched_at;

ALTER TABLE user_entry_state ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
UPDATE user_entry_state SET created_at = (
  SELECT e.created_at FROM entries e WHERE e.id = user_entry_state.entry_id
);

-- Today の絞り込みは published_at ではなく created_at に切り替えるため、
-- published_at 版の部分インデックスは差し替える。
DROP INDEX idx_ues_unread_pub;
CREATE INDEX idx_ues_unread_created
  ON user_entry_state(user_id, created_at DESC)
  WHERE read_at IS NULL;
