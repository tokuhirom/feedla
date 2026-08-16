# feedla マルチユーザー化 設計ドキュメント(構想)

**このドキュメントの位置づけ**: [DESIGN.md](DESIGN.md) は「利用者は自分専用(シングルユーザー)」を
目標に掲げ、「マルチテナント / 一般公開向けアカウント管理」を非目標としている。本ドキュメントは
その前提を緩め、**feedla を少人数マルチユーザー(家庭・小規模チーム規模)で使えるようにする場合の
詳細設計案**をまとめたもの。DESIGN.md の「認証」節の構想([security-review-2026-08.md](security-review-2026-08.md)
で指摘された無認証運用の限界)を発展させた内容であり、**現時点では未実装・構想段階**である。
実装に着手する場合は本ドキュメントを正とし、DESIGN.md の目標・非目標も併せて改訂する。

## 背景と目的

- 現行の feedla は認証を持たず、「リッスンを 127.0.0.1 に限定する」ことだけで保護している。
  家族や少人数チームで同じサーバを共有したい、あるいは Tailscale 等の私設ネットワーク越しに
  複数人で使いたい、というユースケースには応えられない。
- セキュリティレビュー(2026-08)の High 項目が示したとおり、「認証がないこと」自体が
  デプロイ構成のわずかなミスで即座に危険になる構造的弱点でもある。認証基盤の導入は
  シングルユーザー運用の防御強化(Phase A、後述)としても価値がある。

### 目標

- **少人数マルチユーザー**(目安: 数人〜数十人)。同一プロセス・同一 SQLite のまま。
- ユーザーごとに独立した購読・既読・pin・フォルダ・無視ワード。
- **同一フィードの重複クロール回避**(feed は全ユーザーで共有し、クロールは 1 回)。
- 「LDR 的な読み味」= 未読取得の低レイテンシを、マルチユーザー化後も維持する。
- **セキュリティを最優先**: 認証・セッション・CSRF・認可(IDOR)を設計段階で塗り漏れなく決める。

### 非目標(維持するもの)

- 一般公開 SaaS 的なマルチテナント(数百〜数千ユーザー、誰でも自由登録)。
- ソーシャル機能(共有・コメント・フォロー)。`subscriptions.is_public` は引き続き使わない(後述)。
- クラスタリング・水平スケール(シングルノード SQLite のまま)。
- OAuth / OIDC 等の外部 IdP 連携(初期版ではパスワード認証のみ。将来の拡張余地としては残す)。

## 脅威モデルの変化

シングルユーザー時代の脅威モデルは「外部の悪意あるフィード(SSRF・XSS)」と
「被害者ブラウザを踏み台にする CSRF」だけだった。マルチユーザー化で新たに加わるのは:

| 脅威 | 内容 | 主対策(節) |
|---|---|---|
| 水平権限昇格(IDOR) | ユーザー A が B の購読・pin・既読を読み書きする | 認可(§認可) |
| セッション奪取 | Cookie 窃取・セッション固定・推測 | セッション設計(§認証) |
| 認証への総当たり | パスワードブルートフォース・ユーザー列挙 | レート制限(§認証) |
| CSRF | ログイン済みブラウザを踏み台にした状態変更 | CSRF 再設計(§CSRF) |
| 内部ユーザーによる abuse | 大量フィード登録によるクロール負荷・DB 肥大 | クオータ(§リソース制限) |
| 内部ユーザーによる SSRF プローブ | フィード追加/preview を内部ネットワーク探索に使う | dialer 維持 + エラー出し分け(§リソース制限) |
| ユーザー間の情報漏えい | 共有 feeds テーブル経由で他人の購読状況を推測 | §feeds 共有のプライバシー |

「認証があるから安全」ではなく、**認証導入によって初めて発生する脅威**(セッション・CSRF の
Cookie 依存化・IDOR)が大半である点に注意。

## データモデル

### 設計方針

1. **feeds / entries は全ユーザー共有のまま**(客観的データ)。クロールは feed ごとに 1 回。
2. **「ユーザーの主観」はすべて user_id で分離**: 購読・既読・pin・フォルダ・無視ワード。
3. 現行の性能最適化の肝である「部分インデックスで未読取得が O(未読数)」を、
   **user_entry_state への fan-out(書き込み時展開)方式**で維持する(後述の比較参照)。

### 新規テーブル

```sql
-- ユーザー
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,          -- PHC 文字列 ($argon2id$v=19$m=...,t=...,p=...$salt$hash)
  is_admin      INTEGER NOT NULL DEFAULT 0,
  is_disabled   INTEGER NOT NULL DEFAULT 0,  -- 無効化(削除ではなく)
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

-- 招待(招待制登録を使う場合)
CREATE TABLE invitations (
  id           INTEGER PRIMARY KEY,
  token_hash   BLOB NOT NULL UNIQUE,    -- SHA-256(招待トークン)。平文は保存しない
  created_by   INTEGER NOT NULL REFERENCES users(id),
  expires_at   INTEGER NOT NULL,
  used_by      INTEGER REFERENCES users(id),  -- NULL = 未使用
  used_at      INTEGER,
  created_at   INTEGER NOT NULL
);

-- セッション
CREATE TABLE sessions (
  id            INTEGER PRIMARY KEY,
  token_hash    BLOB NOT NULL UNIQUE,   -- SHA-256(セッショントークン)。平文は保存しない
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,       -- idle timeout 判定用
  expires_at    INTEGER NOT NULL        -- absolute timeout
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

-- 非ブラウザクライアント用の API トークン(Fastladder 互換クライアント向け、任意機能)
CREATE TABLE api_tokens (
  id          INTEGER PRIMARY KEY,
  token_hash  BLOB NOT NULL UNIQUE,     -- SHA-256(トークン)
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label       TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  last_used_at INTEGER
);

-- ユーザーごとの記事状態(既読・無視)。read_at を entries から移設する
CREATE TABLE user_entry_state (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entry_id     INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  feed_id      INTEGER NOT NULL,        -- entries.feed_id の非正規化コピー(インデックス用)
  published_at INTEGER NOT NULL,        -- entries.published_at の非正規化コピー(同上)
  read_at      INTEGER,                 -- NULL = 未読
  ignored      INTEGER NOT NULL DEFAULT 0,  -- そのユーザーの ignore_words にヒット
  PRIMARY KEY (user_id, entry_id)
) WITHOUT ROWID;

-- 現行 idx_entries_feed_unread の user 版: 「この購読の未読」が O(未読数)
CREATE INDEX idx_ues_unread
  ON user_entry_state(user_id, feed_id, published_at)
  WHERE read_at IS NULL;

-- 現行 idx_entries_unread_pub (Today グループ) の user 版
CREATE INDEX idx_ues_unread_pub
  ON user_entry_state(user_id, published_at DESC)
  WHERE read_at IS NULL;

-- GC 用: entry 単位で「未読者が残っているか」を引く
CREATE INDEX idx_ues_entry ON user_entry_state(entry_id);
```

### 変更するテーブル

```sql
-- 購読: (user_id, feed_id) の複合主キーへ。folder/rating/unread_count はユーザー単位に
CREATE TABLE subscriptions (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  folder_id    INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  title        TEXT,
  rating       INTEGER NOT NULL DEFAULT 0,
  is_public    INTEGER NOT NULL DEFAULT 0,   -- 引き続き未使用(§非目標との整合)
  unread_count INTEGER NOT NULL DEFAULT 0,   -- キャッシュ(user × feed 単位)
  sort_order   INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (user_id, feed_id)
) WITHOUT ROWID;
CREATE INDEX idx_subscriptions_feed ON subscriptions(feed_id);  -- fan-out・GC・購読者数の逆引き

-- フォルダ: ユーザー単位。name の UNIQUE は (user_id, name) に
CREATE TABLE folders (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  UNIQUE(user_id, name)
);

-- pin: ユーザー単位
CREATE TABLE pins (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entry_id   INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  title      TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, entry_id)
) WITHOUT ROWID;
CREATE INDEX idx_pins_entry ON pins(entry_id);  -- GC の「誰かが pin しているか」判定用

-- 無視ワード: ユーザー単位。word の UNIQUE は (user_id, word) に
CREATE TABLE ignore_words (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  word       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, word)
);

-- entries: read_at / ignored を落とす(user_entry_state へ移設)
-- feeds / scrape_sources / entries_fts はスキーマ変更なし(共有のまま)
```

`entries.read_at`・`entries.ignored` と、それに依存する部分インデックス
(`idx_entries_feed_unread`, `idx_entries_unread_pub`, `idx_entries_gc`)は廃止する。

### 既読状態の持ち方: 2 方式の比較

現行設計の肝は「`entries.read_at` の部分インデックスで未読取得が O(未読数)」。
`read_at` を中間テーブルへ出すとこの最適化が崩れるため、補い方が本設計の最大の分岐点になる。

| | 案 A: 既読行のみ記録(tombstone) | 案 B: 購読時に全記事へ行を展開(fan-out on write)【採用】 |
|---|---|---|
| 行の意味 | 行あり = 既読。未読 = 行なし | 購読中の全記事に行があり、`read_at IS NULL` = 未読 |
| 未読取得 | `entries LEFT JOIN state ... WHERE state.entry_id IS NULL` の anti-join。**O(そのフィードの全記事数)** | 部分インデックス `idx_ues_unread` で **O(未読数)**。現行と同型 |
| Today(全フィード横断未読) | 全購読フィードの anti-join。最悪ケースが重い | `idx_ues_unread_pub` で現行と同じ O(未読数) |
| 書き込み量 | 既読化時のみ。最小 | 新規記事 × 購読者数。500 フィード × 平均 5 記事/日 × 10 ユーザー = 25,000 行/日程度 |
| ストレージ | 最小 | user_entry_state 1 行 ≈ 数十 byte。10 ユーザー × 75,000 記事 ≈ 数十 MB。許容範囲 |
| ignore_words | クエリ時に毎回 LIKE 評価が必要(重い) | fan-out 時にユーザーごとの `ignored` を計算して焼き込める(現行の insert 時計算と同型) |
| GC 判定 | 「行がない = 未読」なので全購読者分の anti-join が必要 | 「`read_at IS NULL` の行が残っているか」を索引で引ける |

**案 B を採用する。** 理由:

- 想定規模(数十ユーザーまで)では書き込み増・ストレージ増が十分小さく、
  一方で読み側のレイテンシ特性(LDR 的な読み味)を現行と完全に同型に保てる。
  SQLite の単一ライタ設計(write conn 1 本 + バッチ tx)とも相性がよい:
  fan-out は `UpsertEntries` と同一トランザクション内の `INSERT ... SELECT` 1 文で済む。
- `ignored` の insert 時計算という現行設計をユーザー単位でそのまま延長できる。
- 案 A は「ユーザー数 × 記事数」が大きくなる将来のための最適化であり、
  非目標(大規模マルチテナント)の領域。必要になった時点で再検討すればよい。

fan-out の実装ポイント:

```sql
-- クローラの UpsertEntries と同一 tx 内で、新規挿入された entry についてのみ実行
INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, read_at, ignored)
SELECT s.user_id, ?, ?, ?, NULL, 0   -- ignored はユーザーごとの ignore_words 判定結果を Go 側で埋める
FROM subscriptions s WHERE s.feed_id = ?;

-- 購読開始時: 既存記事ぶんを一括展開(既読扱いで入れるか未読で入れるかは LDR 互換で「未読」)
INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, read_at, ignored)
SELECT ?, e.id, e.feed_id, e.published_at, NULL, 0
FROM entries e WHERE e.feed_id = ?
ON CONFLICT(user_id, entry_id) DO NOTHING;

-- 購読解除時: そのユーザーの state を掃除(pin は残す/消すを UI で選択)
DELETE FROM user_entry_state WHERE user_id = ? AND feed_id = ?;
```

- `subscriptions.unread_count` キャッシュは現行同様、fan-out・既読化と同一 tx で増減し、
  起動時・バキューム時に `user_entry_state` から再計算して整合させる。
- 再購読時の既読状態は失われる(購読解除で state を消すため)。LDR も同挙動であり許容する。

### feeds 共有の維持とプライバシー

`feeds`(feed_url UNIQUE)は全ユーザーで共有し、同一フィードは 1 回だけクロールする。
2 人目が同じ URL を購読しても `feeds` に行は増えず `subscriptions` に行が増えるだけ。
これは効率上必須の設計として維持する。セキュリティ/プライバシー上の考慮:

- **feeds テーブル自体は購読者情報を一切含まない**(feed_url・title・クロール状態のみの
  客観的データ)ため、feeds の行が他ユーザーに見えても「誰が購読しているか」は漏れない。
- ただし **API の応答設計を誤ると購読の存在が推測できる**。具体的には:
  - 購読追加時に「既にクロール済みの feed だった」ことが応答の速さ(即座に記事が出る)や
    `last_fetched_at` から分かり、「他の誰かが購読している」ことは推測できる。
    これは共有クロールの本質的性質であり、少人数・相互に信頼のあるユーザー群という
    想定の下で**許容するリスク**と明示する(気になる場合は共有をやめるしかなく、
    重複クロール回避と両立しない)。「誰が」までは一切分からないことが設計上の下限。
  - `GET /api/v1/stats` やエラー中フィード一覧は、**自分が購読している feed に限定**して
    返す。全 feed 横断の統計・エラー一覧は admin のみに許可する。
  - フィード検索・オートコンプリートの類で「サーバ内の既存 feeds」を候補提示する機能は
    作らない(他人の購読リストの列挙に等しいため)。
- 購読者が 0 になった feed はクロール停止・GC 対象とする(§GC)。

### scrape_sources(pagewatch)の扱い

`scrape_sources` は feed に 1:1 で付く方式固有設定であり、feed と同様に共有データとする。
ただし `config`(CSS セレクタ等)の**変更は全購読者に影響する**ため:

- `scrape_sources` に `created_by INTEGER REFERENCES users(id)` を追加し、
  **設定の変更(PATCH)・preview は作成者と admin のみ**に許可する。閲覧・購読は誰でも可。
- 同一 `target_url` に別設定で購読したいケースは初期版では非対応(未決事項へ)。

### 全文検索(FTS5)

`entries_fts` は共有のまま。ただし検索 API は**自分の購読フィードの記事に限定**する
(購読していないフィードの記事が検索でだけ見えるのは認可の穴):

```sql
SELECT e.* FROM entries_fts f
JOIN entries e ON e.id = f.rowid
JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = ?
WHERE entries_fts MATCH ?
ORDER BY rank LIMIT ?;
```

## 認証・セッション管理

マルチユーザー化で最重要のセキュリティ論点。「自宅サーバ的に少人数で使う」想定に合わせ、
**自由登録は提供しない**。

### ユーザー登録 / 招待フロー

| 方式 | 採否 | 理由 |
|---|---|---|
| 誰でも登録可能(open registration) | **不採用** | abuse 面(§リソース制限)のコストが目標に見合わない。非目標の「一般公開向け」に踏み込む |
| 管理者がユーザーを直接作成 | **採用(基本)** | 少人数運用では十分。UI は admin 画面 + CLI |
| 招待トークン制 | **採用(任意)** | admin が期限付きトークンを発行 → 被招待者が自分でパスワードを設定。パスワードを admin が知らずに済む |

- **初期セットアップ**: `users` テーブルが空の状態で起動したとき、
  `feedla create-admin` サブコマンド(対話式でパスワード入力)、または初回アクセス時の
  セットアップ画面(users が空の間だけ有効、作成した瞬間に閉じる)で管理者を 1 人作る。
  環境変数でのパスワード受け渡し(`FR_ADMIN_PASSWORD`)は、プロセス一覧・unit ファイルに
  残るため**採用しない**。
- 招待トークンは 128bit 以上の乱数。DB には SHA-256 ハッシュのみ保存し、
  有効期限(既定 72 時間)・使用済みフラグを持つ(スキーマは前掲 `invitations`)。
- ユーザー削除は物理削除ではなく `is_disabled` での無効化を基本とする
  (entries への参照整合と監査のため)。物理削除は admin が明示操作した場合のみ
  (`ON DELETE CASCADE` で購読・state・pin ごと消える)。

### パスワードハッシュ

- **argon2id**(`golang.org/x/crypto/argon2`)。bcrypt は 72 byte 切り詰め問題と
  メモリハードでない点で劣るため採用しない。
- パラメータの目安(OWASP 推奨に準拠、低リソース目標との兼ね合いで調整可):

| パラメータ | 値 | 備考 |
|---|---|---|
| memory | 64 MiB | RAM 100MB 目標に対し大きいが、ログインは低頻度。ログイン処理をセマフォ(同時 1〜2)で直列化して同時実行によるメモリスパイクを抑える |
| iterations (time) | 3 | |
| parallelism | 2 | |
| salt | 16 byte 乱数 | |
| key length | 32 byte | |

- DB には PHC 文字列形式(`$argon2id$v=19$m=65536,t=3,p=2$...`)で保存し、
  パラメータを将来引き上げた際はログイン成功時に透過的に再ハッシュする。
- パスワードポリシー: 最低 12 文字のみ(複雑性ルール・定期変更は課さない。NIST SP 800-63B 準拠)。

### セッション設計

- セッショントークンは **`crypto/rand` の 256bit 乱数**を base64url で Cookie に載せる。
  JWT 等の自己完結型トークンは採用しない(失効できない・実装の罠が多い。
  DB 常駐のサーバでステートレスにする理由がない)。
- **DB には SHA-256(トークン) のみ保存**(前掲 `sessions`)。DB ファイルやバックアップが
  漏れてもセッションを再現できないようにする。
- Cookie 属性:

| 属性 | 値 | 備考 |
|---|---|---|
| 名前 | `__Host-session`(HTTPS 時) / `session`(localhost 平文時) | `__Host-` prefix で Path=/・Secure・ドメイン固定を強制 |
| `HttpOnly` | 必須 | XSS からの窃取防止 |
| `Secure` | HTTPS 時必須 | `FR_COOKIE_SECURE`(既定 auto: リバースプロキシの `X-Forwarded-Proto` は信用せず、明示設定を優先) |
| `SameSite` | `Lax` | CSRF の第一防衛線。`Strict` は外部サイトのリンクから開いた直後に未ログイン表示になる UX 劣化があるため Lax とし、CSRF は §CSRF の多層で担保 |
| `Path` | `/` | |
| `Max-Age` | セッション有効期限と同期 | |

- 有効期限: **idle timeout 30 日**(`last_seen_at` から。アクセスのたびに更新するが、
  書き込み負荷を抑えるため更新は 1 時間に 1 回に間引く)+ **absolute timeout 90 日**
  (`expires_at`。再発行なし、再ログインを要求)。期限切れ行は日次ジョブで削除。
- **セッション固定攻撃対策**: ログイン成功時に必ず新しいトークンを発行し、
  ログイン前のセッション(存在すれば)は削除する。トークンの再利用・引き継ぎはしない。
- **ログアウト**: 該当 sessions 行を削除(サーバ側失効)+ Cookie を `Max-Age=0` で消す。
  「全デバイスからログアウト」(user_id の全セッション削除)も用意する。
  パスワード変更時は当該ユーザーの全セッションを失効させる。
- トークン照合は SHA-256 ハッシュの一致で行う(索引で引けて、かつハッシュ経由なので
  タイミング攻撃で有意な情報は漏れない)。

### ブルートフォース対策

- **アカウント単位 + IP 単位の二重レート制限**をログインエンドポイントに適用:
  - アカウント単位: 直近の連続失敗回数に応じた指数バックオフ(1s, 2s, 4s, ... 上限 15 分)。
    完全ロックは DoS(他人のアカウントを故意にロック)に転用されるため採用しない。
  - IP 単位: 10 回/分 程度の固定ウィンドウ。リバースプロキシ配下では
    `FR_TRUSTED_PROXIES` に列挙した CIDR からの `X-Forwarded-For` のみ信用する
    (未設定時は直接の RemoteAddr を使う。ヘッダを無条件に信じない)。
  - 失敗カウントはメモリ上で管理(プロセス再起動で消えるが、少人数運用では許容)。
- ユーザー列挙対策: 「ユーザーが存在しない」と「パスワードが違う」で
  **応答メッセージ・ステータス・所要時間を揃える**(存在しないユーザーでもダミーの
  argon2id 検証を 1 回実行する)。招待制なので登録経路からの列挙は構造的に存在しない。
- ログイン成否は slog に記録する(username, IP, 成否)。fail2ban 等の外部連携はログ形式のみ保証。

### 認証 API

```
POST   /api/v1/auth/login      {username, password} → Set-Cookie
POST   /api/v1/auth/logout
GET    /api/v1/auth/me          現在のユーザー情報(SPA の初期化用)
POST   /api/v1/auth/password    {current, new} パスワード変更(全セッション失効)
-- admin のみ --
GET    /api/v1/admin/users
POST   /api/v1/admin/users              {username, password} 直接作成
POST   /api/v1/admin/invitations        招待トークン発行
PATCH  /api/v1/admin/users/{id}         無効化/有効化・admin 権限付与
```

- `/api/v1/auth/login`・招待受諾・`/healthz`・`/metrics`(後述)以外の**全エンドポイントを
  認証必須**にする。ミドルウェアで一括適用し、「認証不要リスト」を明示的に持つ
  opt-out 方式(デフォルト保護)にする。ハンドラ個別の opt-in にはしない。
- `/metrics` は運用情報(DB サイズ・フィード数)を含むため、既定で認証必須。
  監視系から cookie なしで叩きたい場合のために `FR_METRICS_TOKEN`(Bearer)を用意する。
- Fastladder 互換クライアント等の非ブラウザクライアントは、セッション Cookie の代わりに
  **ユーザーが発行する API トークン**(前掲 `api_tokens`、`Authorization: Bearer` または
  Fastladder 互換の `ApiKey` フォームパラメータ)で認証する。トークンはユーザー設定画面で
  発行・失効でき、DB にはハッシュのみ保存。**API トークン認証のリクエストは Cookie を
  一切見ない**(CSRF の対象外にするため。§CSRF 参照)。

## CSRF 対策の作り直し

### 現行方式が成立しなくなる理由

現行の `internal/api/csrf.go`(`checkOrigin`)は:

1. `Origin` ヘッダがあり Host と食い違えば 403、
2. **`Origin` が無いリクエストは素通し**(curl・互換クライアント救済)

という 2 段で、「セッションも Cookie もないので、ブラウザ外からのリクエストには
そもそも守るべき ambient authority が無い」という前提に依拠していた。
セッション Cookie を導入するとこの前提が崩れる:

- 守るべきものが「Cookie という ambient authority」に変わるため、
  「ブラウザ外なら素通しでよい」という理屈が消える。素通し穴を残したまま Cookie 認証を
  重ねると、`Origin` を送らない経路(古いブラウザ、一部の埋め込み WebView、
  リダイレクトを介したフォーム送信など Origin が `null`/欠落になるエッジケース)が
  そのまま CSRF 経路になる。
- つまり現行実装は「無認証だから成立していた軽量版」であり、認証導入と同時に作り直しが必要。

### 採用する方式: SameSite=Lax + Origin 検査の必須化(多層防御)

| 方式 | 採否 | 理由 |
|---|---|---|
| SameSite=Lax Cookie | 採用(第一層) | ブラウザ側でクロスサイトの POST に Cookie が付かない。実装コストゼロ |
| Origin 検査(不一致 or **欠落で拒否**に強化) | 採用(第二層) | SameSite 非対応・バグ・サブドメイン経由等への保険。現行実装の判定を「セッション Cookie 認証のリクエストでは Origin 欠落も 403」に変更 |
| 従来型 CSRF トークン(hidden field / ダブルサブミット) | 不採用 | SPA + JSON API では上記 2 層で十分とされる(OWASP も Lax + Origin 検証の組合せを許容)。トークン配布・ローテーションの実装複雑性が上回る |

- 判定ロジック(状態変更メソッドのみ。GET/HEAD/OPTIONS は従来どおり素通し):

```
認証方法が API トークン(Authorization ヘッダ / ApiKey パラメータ)
  → CSRF 検査不要(攻撃者ページはトークンを付与できない。Cookie は見ない)
認証方法がセッション Cookie
  → Origin ヘッダ必須。無い/パースできない/Host と不一致 → 403
```

- 非ブラウザクライアントは API トークン経路に寄せるため、「curl のために Origin 欠落を
  許す」必要がなくなる。**Cookie 経路の素通しは全廃**する。
- 追加の防御として、JSON API(`/api/v1/*`)は `Content-Type: application/json` を検証する
  (`text/plain` を使う JSON-CSRF 手法の芽を摘む。セキュリティレビュー項目 2 の再発防止)。
- リバースプロキシ配下で `Host` が書き換わる構成向けに `FR_PUBLIC_ORIGIN` で
  期待 Origin を明示設定できるようにする(未設定時は `Host` 比較にフォールバック)。

## 認可(IDOR 対策)

全 API エンドポイントで「操作対象が自分のものか」を検証する。マルチユーザー化で
最も塗り漏れが起きやすい箇所なので、**個別ハンドラの if 文ではなく構造で防ぐ**:

### 設計方針

1. 認証ミドルウェアが `user_id` を `context.Context` に格納し、ハンドラは必ずそこから取る。
   リクエストボディ・クエリパラメータ由来の user_id は一切受け付けない(パラメータとして存在させない)。
2. **store 層の関数シグネチャに `userID` を必須引数として追加**し、SQL の WHERE 句に
   常に含める(「認可はクエリに埋め込む」)。`GetSubscription(feedID)` のような
   user 無しシグネチャをコンパイルエラーで排除することで、ハンドラ側のチェック忘れを
   型レベルで防ぐ。
3. 対象が存在しない場合と他人のものだった場合は**同じ 404** を返す(403 で存在を教えない)。

### 各リソースの認可単位

| リソース | 認可の根拠 | 具体的な WHERE |
|---|---|---|
| subscription | 自分の購読か | `subscriptions WHERE user_id = ? AND feed_id = ?` |
| entry の閲覧・既読化 | その entry の feed を購読しているか | `user_entry_state WHERE user_id = ? AND entry_id IN (...)`(fan-out 済みなので state 行の存在 = 購読) |
| pin | 自分の pin か | `pins WHERE user_id = ? AND entry_id = ?` |
| folder | 自分の folder か | `folders WHERE user_id = ? AND id = ?`(subscription 更新時の folder_id 指定も検証) |
| ignore_word | 自分のものか | `ignore_words WHERE user_id = ? AND id = ?` |
| scrape_source の PATCH/preview | 作成者 or admin | `scrape_sources WHERE id = ? AND (created_by = ? OR :is_admin)` |
| OPML export | 自分の購読のみ出力 | `subscriptions WHERE user_id = ?` を起点に生成 |
| stats / エラー一覧 | 自分の購読 feed のみ | `JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = ?` |
| admin API | `users.is_admin = 1` | ミドルウェアで一括 |

### Fastladder 互換 API(`/api/*`)

- `subscribe_id` は実体としては feed_id なので、全ハンドラで
  `(user_id, subscribe_id)` の組で subscriptions を引き直す。行が無ければ 404 相当の
  互換エラー応答(`{"isSuccess": false}` 等、互換クライアントが解せる形)を返す。
- `touch_all` / `unread` / `pin/*` も同様に、context の user_id を必ず伴う store 呼び出しに置換。
- 互換 API は前述のとおり API トークン認証(`ApiKey` パラメータ互換)を第一経路とする。

### v1 API(`/api/v1/*`)

- パスパラメータ `{id}` を使う全エンドポイント
  (`/subscriptions/{id}`, `/subscriptions/{id}/entries`, `/subscriptions/{id}/read_all`,
  `/subscriptions/{id}/refresh`, `/scrape_sources/{id}` 等)は上表の WHERE で引き直す。
- バルク操作(`POST /api/v1/entries/read` の `entry_ids:[...]`)は、
  **リストのうち自分の state 行が存在するものだけを UPDATE** する(存在しない ID は
  黙って無視。1 件ずつ 403 を返すとどの entry_id が実在するかのオラクルになる):

```sql
UPDATE user_entry_state SET read_at = ?
WHERE user_id = ? AND entry_id IN (...) AND read_at IS NULL;
```

- `POST /api/v1/subscriptions/{id}/refresh`(手動即時クロール)は自分が購読している feed に
  限定し、かつユーザー単位のレート制限をかける(§リソース制限。共有 feed への
  クロール強制は他ユーザーにも影響するため)。

## GC / リテンションの見直し

現行の「既読かつ pin 無しで `fetched_at < now - 30日` なら削除」は、記事が複数ユーザーに
共有されると成立しない。**「全購読ユーザーが既読」かつ「誰も pin していない」**に変わる:

```sql
-- 削除対象: 30日経過 かつ 未読者なし かつ pin なし
DELETE FROM entries WHERE id IN (
  SELECT e.id FROM entries e
  WHERE e.fetched_at < :now - 30*86400
    AND NOT EXISTS (          -- 未読のまま残しているユーザーがいない
      SELECT 1 FROM user_entry_state s
      WHERE s.entry_id = e.id AND s.read_at IS NULL
    )
    AND NOT EXISTS (          -- 誰も pin していない
      SELECT 1 FROM pins p WHERE p.entry_id = e.id
    )
  LIMIT :batch
);
-- user_entry_state / pins は ON DELETE CASCADE で追随
```

- `NOT EXISTS` は `idx_ues_entry` / `idx_pins_entry` で索引アクセスになる。
  さらに絞り込みの前段として、案 B では「entry の既読者数 = 購読者数」が成り立つため、
  必要なら `user_entry_state` に `read_at IS NULL` の部分インデックスを entry_id 軸でも
  持てるが、初期版は上記素直な形で始めて実測で判断する。
- **放置ユーザー問題**: 1 人でも読まないユーザーがいると記事が永久に残る。
  上限として現行の「フィードあたり保持件数上限(既定 1000 件)」を**未読でも適用**する
  (超過分は古い順に削除し、該当ユーザーの unread_count を減算)。これで DB サイズの
  上界が壊れない。`is_disabled` なユーザーの未読は GC 判定から除外する。
- **購読者ゼロの feed**: 全ユーザーが購読解除した feed はクロール対象から外し
  (`ClaimDueFeeds` の WHERE に `EXISTS (SELECT 1 FROM subscriptions ...)` を追加)、
  猶予期間(既定 7 日、再購読時のクロール済みデータ再利用のため)ののち feed ごと削除する。

## リソース制限・abuse 対策

招待制なので「見知らぬ悪意ユーザー」は前提にしないが、アカウント乗っ取り・
善意ユーザーの誤操作(巨大 OPML の import 等)への防御として設ける。

### クオータ(ユーザー単位)

| 項目 | 既定値 | 超過時 |
|---|---|---|
| 購読数 | 2,000 | 追加を 4xx で拒否 |
| scrape_sources 作成数 | 50 | 同上 |
| pin 数 | 10,000 | 同上 |
| ignore_words 数 | 1,000 | 同上 |
| OPML import 1 回あたり | 2,000 フィード / 10 MiB | 同上 |
| フィード追加(subscribe + discover) | 60 回/時 | 429 |
| 手動 refresh | 30 回/時 | 429(共有 feed のクロール強制は全員に影響するため) |
| pagewatch preview | 30 回/時 | 429(認証済みユーザーによる外部 URL 取得のため) |
| API 全体 | 600 req/分 | 429(暴走クライアント対策の粗い上限) |

- 値はすべて `FR_QUOTA_*` で調整可能。クオータ判定は COUNT クエリで足りる規模。
- クロール全体の保護(同時 fetch 32・ボディ 10 MiB・ホスト毎 politeness)は現行のまま。
  ユーザーが増えてもクロールは feed 単位で共有されるため、**クロール負荷はユーザー数
  ではなくユニーク feed 数にしかスケールしない**のがこの設計の利点。

### SSRF(マルチユーザーで増える論点)

- 既存の SSRF 対策 dialer(private/loopback/link-local/CGNAT/`0.0.0.0/8`/埋め込み IPv4 拒否、
  redirect 毎ホップ検査)は**そのまま維持**。フィード追加・discover・pagewatch preview は
  すべてこの dialer を通ることを変えない。
- マルチユーザーで増えるのは「認証済みの内部ユーザーが、フィード追加/preview を
  **内部ネットワークの探索オラクル**として使う」リスク:
  - dialer が拒否した場合のエラーメッセージは「取得できませんでした」程度に丸め、
    「private IP のためブロック」等の**判別可能な詳細を返さない**(接続拒否/タイムアウト/
    ブロックの区別がポートスキャンの応答になるため)。詳細は slog にのみ記録する。
  - preview / discover の応答時間の差も原理的にはオラクルになるが、招待制ユーザーの
    脅威度に対して対策(一定遅延の注入)のコストが見合わないため許容する。
- 301/308 による `feed_url` の永続更新は現行どおりだが、更新後 URL も
  スキーム検証(http/https のみ)と feed_url UNIQUE 制約下での衝突処理を維持する。

## 既存の「非目標」との整合性

- **`subscriptions.is_public` は引き続き使わない。** このカラムは LDR/Fastladder のスキーマに
  倣って置かれたまま未使用であり、マルチユーザー化しても有効化しない。
  本設計のスコープは「1 つのサーバを複数人が**それぞれ独立に**使える」ことまでであり、
  以下はすべて**引き続き非目標**として線を引く:
  - 購読リストの公開・他ユーザーへの共有(is_public の本来の用途)
  - 他ユーザーの「読んでいる記事」「レート」の閲覧、フォロー、おすすめ
  - 記事への注釈・コメント
- 理由: ソーシャル機能は認可モデルを「所有者のみ」から「公開範囲」へ複雑化させ、
  本ドキュメントで積み上げた IDOR 対策(user_id を全クエリに埋め込む)の単純さを壊す。
  やるとしても本設計の安定後に別ドキュメントで設計する。
- ユーザー間で共有されるのは feeds/entries/scrape_sources(客観データ)のみ、という
  不変条件を保つ。「主観データはすべて user_id 付き」がレビュー時の判定基準になる。

## マイグレーション戦略

既存のシングルユーザー DB を、無停止前提なし(feedla は再起動が軽い)の
起動時マイグレーションで移行する。SQLite は `ALTER TABLE` が貧弱なので、
主キー変更を伴うテーブルは作り直し(create → copy → drop → rename)になる。

```sql
-- 0005_multi_user.sql(概略)

-- 1. users を作成し、デフォルト管理者を挿入
--    パスワードは無効値にしておき、初回起動時のセットアップで設定させる
--    (マイグレーション SQL に平文/ハッシュを焼き込まない)
CREATE TABLE users (...);
INSERT INTO users (id, username, password_hash, is_admin, created_at, updated_at)
VALUES (1, 'admin', '!locked!', 1, strftime('%s','now'), strftime('%s','now'));

-- 2. sessions / invitations / api_tokens / user_entry_state を作成
CREATE TABLE sessions (...);
...

-- 3. 既存データを user_id = 1 に割り当てて各テーブルを作り直し
CREATE TABLE subscriptions_new (...);   -- 前掲の新スキーマ
INSERT INTO subscriptions_new (user_id, feed_id, folder_id, title, rating,
                               is_public, unread_count, sort_order, created_at)
SELECT 1, feed_id, folder_id, title, rating, is_public, unread_count, sort_order, created_at
FROM subscriptions;
DROP TABLE subscriptions;
ALTER TABLE subscriptions_new RENAME TO subscriptions;
-- folders / pins / ignore_words も同様に user_id = 1 を焼き込んで作り直し

-- 4. entries.read_at / ignored を user_entry_state へ移設
INSERT INTO user_entry_state (user_id, entry_id, feed_id, published_at, read_at, ignored)
SELECT 1, e.id, e.feed_id, e.published_at, e.read_at, e.ignored
FROM entries e
WHERE EXISTS (SELECT 1 FROM subscriptions s WHERE s.feed_id = e.feed_id AND s.user_id = 1);

-- 5. entries から read_at / ignored と旧部分インデックスを除去(テーブル作り直し)
--    ※ entries_fts は content='entries' の external content のため、
--      作り直し後に INSERT INTO entries_fts(entries_fts) VALUES('rebuild') で再構築し、
--      同期トリガも張り直す

-- 6. scrape_sources に created_by を追加(既存行は 1)
ALTER TABLE scrape_sources ADD COLUMN created_by INTEGER NOT NULL DEFAULT 1 REFERENCES users(id);
```

運用手順・注意点:

- マイグレーション全体を 1 トランザクションで実行(自前ランナーの既存挙動に準拠)。
  実行前に `VACUUM INTO` バックアップを必ず取る(日次バックアップ機構を流用)。
- `admin` の `password_hash = '!locked!'` は argon2id として決してマッチしない番兵値。
  この状態のユーザーがいる間はログインを拒否し、`feedla create-admin --reset` または
  初回セットアップ画面(users に有効なパスワードのユーザーが 0 人の間だけ有効)で
  パスワードを設定させる。**「マイグレーションした瞬間に無認証で admin になれる」時間を
  作らない**ことが要点(認証導入前のシングルユーザー運用は 127.0.0.1 限定が前提なので、
  セットアップ画面へ最初に到達できるのはサーバ管理者本人、という前提は従来から変わらない)。
- 既存ユーザー(=これまでの唯一の利用者)から見ると、移行後は「admin としてログインすると
  今までの購読・既読・pin がすべてそのまま見える」状態になる。
- ロールバックはバックアップからのリストアのみサポート(逆方向マイグレーションは書かない)。

## 段階的導入

一気に全部を入れず、各 Phase が単独でリリース可能・単独で価値を持つ形に分割する。

| Phase | 内容 | リリース時点での姿 | 完了条件 |
|---|---|---|---|
| A | **認証基盤**: users(1 人だけ)・sessions・login UI・argon2id・レート制限・CSRF 再設計(SameSite+Origin 必須化)・API トークン | シングルユーザーのまま、外部公開に耐える認証付き feedla。DESIGN.md「認証」節の構想の実装に相当 | 127.0.0.1 縛りなしで安全に公開できる。既存 e2e が認証付きで通る |
| B | **データモデル分離**: user_entry_state への fan-out・subscriptions/folders/pins/ignore_words の user_id 化・マイグレーション 0005・store 層シグネチャの userID 必須化・GC 新条件 | 外形は Phase A と同じ(ユーザーは 1 人)。内部が完全に user_id 体系になる | 全 store 関数が userID を要求する。移行後のベンチで未読取得のレイテンシが現行同等 |
| C | **マルチユーザー本体**: admin 画面・招待フロー・複数ユーザーの認可(IDOR)テスト・クオータ・stats のユーザー限定 | 複数人で使える | 2 ユーザーでの e2e(相互に見えない・操作できないことのテストを含む) |

- Phase A を先行させるのは、認証・CSRF はデータモデルと独立に導入でき、
  かつ**シングルユーザー運用の防御強化として単体で価値がある**ため
  (セキュリティレビュー High 項目の根本対策)。
- Phase B は「見た目が変わらないリファクタリング + マイグレーション」であり、
  性能リグレッションの検証に集中できる。IDOR 対策の型レベル強制(store シグネチャ)も
  ここで入れておくことで、Phase C は「ユーザーを増やしても壊れないことの検証」に絞れる。
- 各 Phase の完了時に security-review 相当のレビュー(認可の塗り漏れ・セッション・
  レート制限の観点)を実施する。

## 未決事項 / 今後の検討

1. **同一 target_url への別設定 pagewatch**(§scrape_sources)。scrape_source を
   ユーザー所有に倒すか、feed 側を分岐させるか。
2. **2 要素認証(TOTP)**。招待制・少人数の脅威モデルでは初期版から入れる必然性は薄いが、
   sessions 設計は将来の追加を妨げない。
3. **外部 IdP(OIDC)連携**。Tailscale/リバースプロキシの認証ヘッダ
   (`Remote-User`)を信用するモードの方が自宅サーバ用途には合うかもしれない。
4. **Fever API / Google Reader API 互換層**(DESIGN.md 未決事項 4)を載せる場合の
   認証は api_tokens をそのまま使える見込み。
5. **ユーザーごとのクロール設定**(fetch_interval の希望値が衝突した場合の解決)。
   初期版は「最も短い希望値を採用」等はせず、feed のグローバル設定のみとする。
