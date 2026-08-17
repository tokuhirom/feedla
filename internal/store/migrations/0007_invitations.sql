-- Phase C 招待フロー: admin が期限付きトークンを発行し、被招待者が自分で
-- パスワードを設定してアカウントを作成する(docs/multi-user-design.md
-- 「ユーザー登録 / 招待フロー」節)。スキーマは同ドキュメント掲載のものを
-- そのまま採用。

CREATE TABLE invitations (
  id           INTEGER PRIMARY KEY,
  token_hash   BLOB NOT NULL UNIQUE,
  created_by   INTEGER NOT NULL REFERENCES users(id),
  expires_at   INTEGER NOT NULL,
  used_by      INTEGER REFERENCES users(id),
  used_at      INTEGER,
  created_at   INTEGER NOT NULL
);
