# feedla 設計ドキュメント

開発者向けの内部設計資料。利用者向けの情報は [README.md](../README.md) を参照。

## 背景と目的

livedoor Reader (以下 LDR) / Fastladder のような「大量のフィードを高速に読み流す」体験を、
現代的なスタックで再実装する。

### 目標

- **シングルバイナリ**で動く。ランタイム・外部 DB・外部ジョブキュー不要。
- **低リソース**。数百〜千フィード規模を RAM 100MB 以下・常時 CPU ほぼゼロで回す。
- **シングルノード**完結。水平分散は非目標。
- **フィード取得は並列・非同期**。1 本の遅いサーバが全体を止めない。
- **LDR 風の 3 ペイン Web UI**。キーボード操作で「読み流す」体験を再現。
- 利用者は**自分専用（シングルユーザー）**。

### 非目標

- マルチテナント / 一般公開向けアカウント管理(少人数マルチユーザー化の構想は
  [multi-user-design.md](multi-user-design.md) 参照。Phase A(認証基盤)は実装済みだが、
  Phase B(データモデルの user_id 分離)・Phase C(複数ユーザー本体・招待制・admin 画面)は
  未実装であり、引き続き非目標の範囲)
- クラスタリング・水平スケール
- ソーシャル機能(共有、コメント、フォロー)
- モバイルネイティブアプリ(Web UI をレスポンシブにする程度に留める)

## 用語

| 用語 | 意味 |
|---|---|
| Feed | 購読対象の RSS/Atom Feed。URL 単位で一意。 |
| Subscription | ユーザーから見た購読。本設計では Feed と 1:1 だが、フォルダ・レートなど「ユーザー側の属性」を分離して保持する。 |
| Entry | フィード内の 1 記事。 |
| Crawler | フィードを取得する仕組み全体。 |
| Fetcher | HTTP 取得を行う worker。 |
| Scheduler | 「次に取りに行くべきフィード」を決める仕組み。 |
| touch_all | あるフィードの未読を一括既読にする LDR 由来の操作。 |
| pin | 「あとで読む」に積む LDR 由来の機能。 |

## 全体アーキテクチャ

```
                   ┌───────────────────────────────────────────┐
                   │            single Go process              │
                   │                                           │
  Browser  ─HTTP──▶│  HTTP Server (net/http)                   │
  (SPA)            │    ├─ /            embed した SPA         │
                   │    └─ /api/*       JSON API               │
                   │            │                              │
                   │            ▼                              │
                   │      Service Layer                        │
                   │            │                              │
                   │  ┌─────────┴──────────┐                   │
                   │  ▼                    ▼                   │
                   │ Store (SQLite)   Crawler                  │
                   │  ├ read pool      ├ Scheduler (ticker)    │
                   │  └ write conn(1)  ├ Fetch queue (chan)    │
                   │                   ├ Fetcher pool (N)      │
                   │                   ├ Parser                │
                   │                   └ Writer (batch tx)     │
                   └───────────────────────────────────────────┘
                                        │
                                        ▼ HTTPS
                                   外部フィードサーバ
```

- プロセスは 1 つ。Web サーバとクローラが同居する。
- 永続化は SQLite ファイル 1 つ（+ WAL）。
- フロントエンドはビルド済み静的ファイルを `embed.FS` でバイナリに同梱。

### 「ノンブロッキング IO」について

Go では goroutine + netpoller (epoll/kqueue) により、`net/http` 呼び出しは
**言語ランタイムレベルで既にノンブロッキング多重化されている**。
したがって「明示的に非同期 IO ライブラリを使う」のではなく、

- goroutine を fetch 単位で立て、
- その数を **有界な worker pool / semaphore で制御する**

という形にする。これで OS スレッドは数個に張り付いたまま数百接続を並行処理できる。
注意すべきは goroutine のばら撒きではなく、**同時接続数と 1 リクエストあたりのメモリ上限**の管理。

## 技術選定

| 領域 | 採用 | 理由 / 代替案 |
|---|---|---|
| 言語 | Go 1.26+ | 決定事項。単一バイナリ・GC・netpoller が要件に合う。 |
| HTTP サーバ | 標準 `net/http` + `http.ServeMux` (1.22 以降のパターンルーティング) | 依存を増やさない。複雑化したら `chi` を検討。 |
| DB | SQLite (WAL モード) | 単一ノード・低リソース・バックアップ容易。 |
| SQLite ドライバ | `modernc.org/sqlite` (pure Go) | cgo 不要でクロスコンパイルが容易。FTS5 も同梱。性能が問題なら `mattn/go-sqlite3` に差し替え可能なよう `database/sql` 越しに抽象化。 |
| マイグレーション | 自前ランナー（`internal/store/migrate.go`） | `//go:embed migrations/*.sql` で SQL ファイルを埋め込み、`schema_migrations` テーブルで適用済みを管理して起動時に自動適用。依存を増やさないため dbmate/golang-migrate は不採用。 |
| フィードパーサ | `mmcdole/gofeed` | RSS 0.9x/1.0/2.0, Atom 0.3/1.0, JSON Feed を一括対応。自前実装は互換性地獄なので避ける。 |
| HTML サニタイズ | `microcosm-cc/bluemonday` | 本文表示時の XSS 対策。 |
| ログ | 標準 `log/slog` (JSON) | 依存ゼロ。 |
| 設定 | 環境変数（`FR_*`） | 自分専用なので簡素に。TOML は未採用。 |
| フロントエンド | TypeScript + Preact + `@preact/signals` + Vite | 軽量。React でも可だがバンドルサイズを抑えたい。状態は signal に集約。 |

## データモデル

### スキーマ

```sql
-- フィード自体（クローラが管理する客観的情報）
CREATE TABLE feeds (
  id              INTEGER PRIMARY KEY,
  feed_url        TEXT NOT NULL UNIQUE,
  site_url        TEXT,
  title           TEXT NOT NULL DEFAULT '',
  description     TEXT NOT NULL DEFAULT '',
  favicon_url     TEXT,

  -- 条件付き GET 用
  etag            TEXT,
  last_modified   TEXT,
  body_hash       BLOB,            -- 正規化後の本文ハッシュ（304 非対応サーバ向け）

  -- スケジューリング
  fetch_interval_sec INTEGER NOT NULL DEFAULT 1800,
  next_fetch_at   INTEGER NOT NULL,   -- unix time
  last_fetched_at INTEGER,
  last_success_at INTEGER,
  last_status     INTEGER,            -- 直近の HTTP status
  error_count     INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT,

  -- 更新頻度の推定用
  avg_entries_per_day REAL NOT NULL DEFAULT 0,

  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_feeds_next_fetch ON feeds(next_fetch_at) WHERE error_count < 20;

-- 購読（ユーザー側の属性）。シングルユーザーだが feed とは分離しておく
CREATE TABLE subscriptions (
  feed_id      INTEGER PRIMARY KEY REFERENCES feeds(id) ON DELETE CASCADE,
  folder_id    INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  title        TEXT,               -- ユーザーによる上書き
  rating       INTEGER NOT NULL DEFAULT 0,   -- LDR の ★ 相当 (0..5)
  is_public    INTEGER NOT NULL DEFAULT 0,
  unread_count INTEGER NOT NULL DEFAULT 0,   -- キャッシュ
  sort_order   INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL
);

CREATE TABLE folders (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  sort_order INTEGER NOT NULL DEFAULT 0
);

-- 記事
CREATE TABLE entries (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid         TEXT NOT NULL,       -- guid/id、無ければ link、無ければ hash(title+content)
  url          TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  author       TEXT NOT NULL DEFAULT '',
  body         TEXT NOT NULL DEFAULT '',   -- サニタイズ済み HTML
  body_hash    BLOB NOT NULL,
  published_at INTEGER NOT NULL,    -- フィードに無ければ初回取得時刻
  updated_at   INTEGER NOT NULL,
  fetched_at   INTEGER NOT NULL,
  read_at      INTEGER,             -- NULL = 未読
  UNIQUE(feed_id, guid)
);
CREATE INDEX idx_entries_feed_pub    ON entries(feed_id, published_at DESC, id DESC);
CREATE INDEX idx_entries_feed_unread ON entries(feed_id, published_at) WHERE read_at IS NULL;
CREATE INDEX idx_entries_gc          ON entries(fetched_at) WHERE read_at IS NOT NULL;

-- あとで読む
CREATE TABLE pins (
  entry_id   INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  title      TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

-- 全文検索（external content table で二重持ちを回避）
CREATE VIRTUAL TABLE entries_fts USING fts5(
  title, body,
  content='entries', content_rowid='id',
  tokenize="trigram"          -- 日本語対応のため trigram
);
-- INSERT/UPDATE/DELETE 同期トリガを別途定義
```

### 設計上のポイント

- **`entries.read_at` に既読を持つ**（シングルユーザーなので join 不要）。
  部分インデックス `WHERE read_at IS NULL` により未読件数取得が O(未読数)。
- **`subscriptions.unread_count` はキャッシュ**。書き込み時に一緒に更新し、
  起動時とバキューム時に再計算して整合させる。フィード一覧の描画で毎回 COUNT しないため。
- **重複判定は `(feed_id, guid)`**。guid が毎回変わる悪質なフィードのために
  `body_hash` によるフォールバック照合も入れる（同一 feed 内で url 一致 かつ hash 一致なら同一とみなす）。
- **日本語全文検索**は FTS5 の `trigram` トークナイザで対応。
  形態素解析（bleve + kagome など）は依存が重いので初期版では採用しない。

### SQLite の扱い

```
PRAGMA journal_mode = WAL;
PRAGMA synchronous  = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA cache_size   = -20000;   -- 20MB
```

- **`*sql.DB` を 2 本持つ**:
  - read 用: `SetMaxOpenConns(runtime.NumCPU())`, read-only で open
  - write 用: `SetMaxOpenConns(1)` — SQLite は単一ライタなので、
    アプリ側で直列化して `SQLITE_BUSY` を構造的に発生させない。
- クローラの書き込みは **バッチトランザクション**（フィード単位、または 100 件 / 200ms でまとめる）。

## クローラ設計

### パイプライン

実装（`internal/crawler/crawler.go` の `crawlFeeds`）は、ステージを分離した
channel パイプラインではなく、**1 feed = 1 goroutine が fetch → parse → write
まで同期的にやり切る**シンプルな構成。並列度は `chan struct{}` によるセマフォ
（既定 `FR_FETCH_CONCURRENCY=32`）で頭打ちにする。

```
 Scheduler(30s tick) ──▶ ClaimDueFeeds(limit) ──▶ semaphore(cap=N) で goroutine を起動
                                                    各 goroutine: fetch → parse → 自分の分だけ書き込み
```

書き込みはフィード単位のトランザクション（`UpsertEntries`）で行われ、複数フィード
分をまとめてバッチ化する専用 Writer ステージは無い。段階分離によるステージ別の
並列度チューニングは行わず、セマフォ 1 本で全体の同時実行数のみ制御する設計。

### Scheduler

- `internal/crawler/scheduler.go` の `Scheduler.Run` が既定 30 秒 tick で
  `Crawler.CrawlDue` を呼ぶ。
- due な feed の選定と二重投入防止は `internal/store/feeds.go` の
  `ClaimDueFeeds`（`UPDATE ... RETURNING` で `next_fetch_at <= ?` な feed を
  選択すると同時に `next_fetch_at = now + fetch_interval_sec` へ仮更新する
  原子的な SQL 1 本）で行う。メモリ上の in-flight set は使っていない。
- 起動直後に全フィードが一斉に due になるのを避けるため、
  初回登録時に `next_fetch_at` へ **ジッタ（0〜interval のランダム）** を入れる
  （`internal/store/feeds.go`）。

### 取得間隔の適応制御

LDR 同様、更新頻度に応じて巡回間隔を変える。

```
成功 & 新規記事あり : interval = max(MIN, interval * 0.7)
成功 & 新規記事なし : interval = min(MAX, interval * 1.3)
304 Not Modified   : interval = min(MAX, interval * 1.3)   (コストは低いので緩やかに)
エラー             : interval = min(MAX_ERR, base * 2^min(error_count, 8)) + jitter
```

- `MIN = 10min`, `MAX = 12h`, `MAX_ERR = 24h`
- `429` / `503` は **`Retry-After` を最優先で尊重**。`Retry-After` から算出した
  interval は他の場合と同様に即座に `feeds.next_fetch_at`/`fetch_interval_sec`
  へ書き込まれる（`UpdateFeedAfterFetch`）ため、SQLite に永続化されプロセス
  再起動をまたいでも保持される。
- `error_count >= 20` かつ 30 日以上成功なしのフィードは「停止中」フラグを立てて
  巡回対象から外し、UI に表示して手動再開できるようにする。

### Fetcher

```go
type Fetcher struct {
    client   *http.Client
    hostSem  *HostSemaphore  // ホストごとの同時接続数制限
    maxBody  int64           // 10 MiB
}
```

**HTTP クライアント設定**

```go
transport := &http.Transport{
    Proxy:                 http.ProxyFromEnvironment,
    DialContext:           safeDialer.DialContext, // SSRF 対策（後述）
    MaxIdleConns:          100,
    MaxIdleConnsPerHost:   2,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:   10 * time.Second,
    ResponseHeaderTimeout: 20 * time.Second,
    ForceAttemptHTTP2:     true,
}
client := &http.Client{
    Transport:     transport,
    Timeout:       60 * time.Second,          // 全体 deadline
    CheckRedirect: limitRedirects(5),
}
```

**リクエスト側**

- `If-None-Match` / `If-Modified-Since` を必ず送る（帯域と相手サーバへの負荷を大幅削減）。
- `Accept-Encoding: gzip`（`net/http` の自動展開に任せる）。
- `User-Agent` に製品名と連絡先 URL を含める。
- `context.WithTimeout` で各ジョブに deadline。

**レスポンス側**

- `io.LimitReader(resp.Body, maxBody)` で読み込み上限。超過はエラー扱い。
- ステータス別:
  - `200` → パースへ
  - `304` → 何もせず interval だけ更新（最安コスト）
  - `301/308` → `feed_url` を新 URL に永続的に更新
  - `410` → 購読を「配信終了」としてマーク
  - `4xx/5xx` → error_count++

**礼儀（politeness）**

- 同一ホストへの同時接続は 2 まで（`HostSemaphore`: `map[string]chan struct{}` + mutex、
  未使用エントリは定期 GC）。
- 同一ホストへの連続リクエストは最低 1 秒空ける。
- 全体の同時 fetch 数はデフォルト 32（設定可能）。

### Parser

- `gofeed.Parser` に `io.Reader`(fetch した body)を渡す。charset 判定は
  `gofeed`/内部の `mmcdole/goxpp` に任せており、feedla 自身が
  `golang.org/x/net/html/charset` 等を別途呼び出すことはしていない。
- 正規化処理:
  - 相対 URL を feed の base URL で絶対化。解決後のスキームが `http`/`https`
    以外（`javascript:`/`data:` 等）の場合は空文字列に落として破棄する
    （`entry.url`/`feeds.site_url` いずれも対象）。
  - `published` が無い / 未来 / 極端に古い場合の補正（未来 → now、無し → 初回取得時刻）。
  - 本文は `content:encoded` > `content` > `description` > `summary` の優先順で採用。
  - `bluemonday.UGCPolicy()` ベースのサニタイズ（`script`, `iframe`, `on*`, `style` 除去）。
    `img` は `AllowImages()` の既定どおり `src`/`alt`/`align`/`height`/`width` を許可し、
    `loading="lazy"` の付与は行っていない（bluemonday は属性を除去するのみで付与はしない）。
- 1 フィードあたりの取り込み上限 1000 件（DoS 対策）。
- パース（`internal/crawler/parser.go`）は fetch した goroutine の中でそのまま
  同期的に行う。専用の Parser/Writer ステージには分離していない（前掲「パイプ
  ライン」参照）。

### 書き込み

- `internal/store/entries.go` の `UpsertEntries` が**フィード単位で 1 トランザ
  クション**。
- `INSERT ... ON CONFLICT(feed_id, guid) DO NOTHING` を使い、`RowsAffected` で
  新規/既存を判別する。既存記事の body は上書きせず、**既読済み記事は再取得
  しても未読に戻さない**（LDR の挙動に合わせる）。
- 同一 tx 内で `subscriptions.unread_count` キャッシュも更新。

### GC / リテンション

日次のバックグラウンドジョブ:

- 既読かつ pin されていない記事のうち、`fetched_at < now - 30日` を削除。
- フィードあたりの保持件数上限（既定 1000 件）を超える古い既読記事を削除。
- `PRAGMA optimize` を実行。週次で `VACUUM INTO` でバックアップ兼断片化解消。

## API 設計

**LDR 互換 API を用意しておくと既存クライアント資産が使える**ため、
`/api/*` は Fastladder 互換、新規機能は `/api/v1/*` に置く二段構えとする
(認証は `/api/*`・`/api/v1/*` を問わず共通のミドルウェアで一律に必須にする。
§セキュリティ/認証)。

### Fastladder 互換エンドポイント（POST, form-encoded, JSON 応答）

| Endpoint | 説明 |
|---|---|
| `POST /api/subs?unread=1` | 購読一覧（未読ありのみ / 全件） |
| `POST /api/unread` (`subscribe_id`) | 指定購読の未読記事一覧 |
| `POST /api/touch_all` (`subscribe_id`) | 一括既読 |
| `POST /api/pin/add` (`link`, `title`) | pin 追加 |
| `POST /api/pin/remove` (`link`) | pin 削除 |
| `POST /api/pin/all` | pin 一覧 |
| `POST /api/subscribe` (`feedlink`) | 購読追加 |
| `POST /api/unsubscribe` (`subscribe_id`) | 購読解除 |
| `POST /api/folders` | フォルダ一覧 |

### 新 API（`/api/v1`, JSON body）

```
GET    /api/v1/subscriptions                 購読一覧（未読数つき）
POST   /api/v1/subscriptions                 {url} 追加（feed 自動検出）
PATCH  /api/v1/subscriptions/{id}            フォルダ/レート/タイトル変更
DELETE /api/v1/subscriptions/{id}
GET    /api/v1/subscriptions/{id}/entries    ?unread=1&limit=100&cursor=...
POST   /api/v1/entries/read                  {entry_ids:[...]} 一括既読
POST   /api/v1/subscriptions/{id}/read_all   {before: ts}
GET    /api/v1/search                        ?q=...&limit=&cursor=   (FTS5)
POST   /api/v1/subscriptions/{id}/refresh    手動即時クロール
GET    /api/v1/pins  /  POST  /  DELETE
GET    /api/v1/opml  /  POST /api/v1/opml    OPML の入出力
GET    /api/v1/stats                         クロール統計・エラー一覧
GET    /api/v1/scrape_sources  /  POST  /  PATCH {id}        フィード非提供ページの監視登録（pagewatch）
POST   /api/v1/scrape_sources/{id}/preview   保存・差分判定なしの読み取り専用プレビュー
GET    /healthz  /  /metrics
```

- ページングは `(published_at, id)` の複合カーソル（offset は使わない）。
- 既読送信は**クライアント側でデバウンスしてバルク POST**（1 記事 1 リクエストにしない）。
- **フィードのないページを購読する（通称 pagewatch）**: `internal/extract/pagewatch` が
  ページの差分をエントリーとして合成し、`feeds.feed_url` に `pagewatch:` 疑似スキームを
  付けて通常のフィードと同じクロール経路に乗せる。`SubscriptionView.kind` が
  `"feed"`/`"pagewatch"` を区別する。`pagewatch:` は feedla 内部でしか意味を持たない
  ため、**OPML export/import の両方でこの種の購読は除外する**（他ツールへの
  持ち出しでは壊れた xmlUrl になり、手編集/他ツール由来の import では対応する
  `scrape_sources` 行がなくクロールできないため）。詳細設計は
  [feedless-site-subscription-pagewatch.md](feedless-site-subscription-pagewatch.md) を参照。

### フィード自動検出（subscribe 時）

1. 与えられた URL を取得。
2. Content-Type / 中身がフィードならそのまま採用。
3. HTML なら `<link rel="alternate" type="application/rss+xml|atom+xml|feed+json">` を抽出。
4. 候補が複数なら UI に選択肢を返す（`202` + 候補リスト）。
5. 候補が 0 件ならエラーを返す。`internal/feed/discover.go` の `DiscoverFeed` は
   現状ここまで。

## Web UI

### レイアウト（LDR 風 3 ペイン）

```
┌──────────────┬─────────────────────────────────────────────┐
│ フォルダ /   │  [フィードタイトル]  未読 32   ★★★  ⟳  詳細 │ ← ヘッダ
│ 購読リスト   ├─────────────────────────────────────────────┤
│              │                                             │
│ ▸ Tech (128) │   記事タイトル                              │
│   ├ feed A 12│   ─────────────────────────────             │
│   ├ feed B  3│   本文（連続表示）                          │
│ ▸ 日記  (44) │                                             │ ← 記事ペイン
│   ...        │   ───────────────────────────               │
│              │   次の記事タイトル                          │
│              │   本文...                                   │
└──────────────┴─────────────────────────────────────────────┘
```

LDR の本質は「1 購読ぶんの未読を**縦に連続表示**し、`j`/`k` で流し読みして
`s` で次の購読に移る」体験なので、これを最優先で再現する。

購読解除は、ヘッダーの常時表示ボタン(再クロールや前後移動と隣接し誤タップしやすい)
には置かず、「詳細」ボタンから開く**フィード詳細画面**(`FeedDetailOverlay`)に
「購読解除」という文言のボタンとして配置する。押しても `window.confirm` の確認を
挟むまで実行されない(`actions.ts` の `unsubscribeFeed`)。同じ詳細画面に、その
フィードの最終取得時刻・次回取得予定時刻(`SubscriptionView.last_fetched_at`/
`next_fetch_at`)も表示する。

サイドバー下部の「クロール状況」ボタンからは、既存の `GET /api/v1/stats` を叩いて
購読フィード数・エラー中フィード数・未読記事数・次回巡回待ち(due)フィード数・
DB サイズを表示する(`StatsOverlay`)。API 自体は Phase6 で実装済みだったが、
Web UI からは見えていなかったギャップを埋めた。

ヘッダーの ★☆☆☆☆ はクリック/タップで評価を変更できる(`PATCH
/api/v1/subscriptions/{id}` は Phase5 から実装済みだったが、UI 側からは
一度も呼ばれていなかった)。同じ星をもう一度押すと 0 に戻る。

### モバイル対応

`j`/`k` 等はハードウェアキーボード前提のため、幅 700px 未満のビューポートでは
以下のように振る舞いを変える(`web/src/styles/global.css` の `@media (max-width:
700px)` ブロック、`web/src/hooks/useAutoMarkRead.ts`):

- 2 カラムグリッドではなく、サイドバー(購読一覧)と記事ペインを**1 画面ずつの
  単一カラム表示**に切り替え、記事ペインのヘッダーに「‹ 一覧」の戻るボタンを出す。
  どちらを表示するかは選択中のフィード有無(`selectedFeedId`)で決まる。
- **スクロールで既読**: `IntersectionObserver` で記事の下端がペイン上端を
  過ぎたら自動的に既読にする(`markReadOptimistic` を叩くのは j と同じ経路)。
  タップ操作だけで未読を消化できる。デスクトップでも同時に有効(j による
  「読み終えたら次へ」という既存のセマンティクスと矛盾しないため)。
  ただし**リスト最後の記事は「これより下へスクロールして押し出す」ことが
  構造的にできない**ため、ペインが実際に最下端までスクロールされたことを
  別途検知し、そのタイミングで読み込み済みの記事を一括で既読化するフォール
  バックを併設している。
- ヘッダーに前後のフィードへ移動するボタン(`‹`/`›`、`a`/`s` のタップ版)を追加。
- タップ領域(購読行・ヘッダーのボタン等)を最低 44px に拡大。

### パフォーマンス設計

- **先読み**: 現在の購読を表示中に、次の購読の未読をバックグラウンド取得しておく
  （LDR の体感速度の肝）。
- **既読の楽観的更新**: UI 上は即座に既読化し、サーバへは 2 秒デバウンスでバルク送信。
  失敗時はリトライキューに戻す。
- 1 購読あたりの未読は 100 件ずつ取得し、スクロール末尾で追加ロード。
- DOM は仮想スクロールまでは不要（100 件程度）。ただし表示済み記事の
  `content-visibility: auto` は入れる。
- SPA バンドルは gzip 後 100KB 以下を目標。

### セキュリティ（表示面）

- 本文はサーバ側でサニタイズ済み。加えて CSP を設定:
  `default-src 'self'; img-src 'self' https: data:; script-src 'self'; frame-src https://www.instagram.com`
  (`frame-src` は Instagram の単一投稿 embed ページ専用。詳細は
  [docs/adr/0001-third-party-embed-in-feed-content.md](adr/0001-third-party-embed-in-feed-content.md))
- 外部画像はデフォルトで直接読み込む（自分専用のため）。
  トラッキングが気になる場合のオプションとして**画像プロキシ**（`/img?url=...`、
  署名付き、サイズ上限、private IP 拒否）を検討したが、**未実装**。
  `internal/api/` に該当ハンドラは無い。

## セキュリティ / 堅牢性

### SSRF 対策

自分専用でも、悪意あるフィードのリダイレクト先が内部ネットワークを指す可能性がある。

```go
safeDialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
// Control で「実際に接続する IP」を検査する（DNS rebinding 対策）
safeDialer.Control = func(network, address string, c syscall.RawConn) error {
    host, _, _ := net.SplitHostPort(address)
    ip := net.ParseIP(host)
    if ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
       ip.IsLinkLocalUnicast() || ip.IsUnspecified() || isCGNAT(ip) {
        return errBlockedAddress
    }
    return nil
}
```

- スキームは `http`/`https` のみ許可。
- リダイレクトは 5 回まで、各ホップで同じ検査を通す（`Control` なので自動的に効く）。
- 169.254.169.254 (メタデータ) を含む private/link-local は全拒否。

### リソース保護

| 項目 | 上限 |
|---|---|
| レスポンスボディ | 10 MiB |
| 1 フィードの取り込み記事数 | 1000 |
| 記事本文 | 512 KiB（超過は切り詰め） |
| 同時 fetch | 32（設定可） |
| 1 ジョブの deadline | 60s |

- XML 爆弾対策: `gofeed` は `encoding/xml` ベースで外部実体参照を展開しないが、
  加えて上記のサイズ上限で防御する。

### 認証

[docs/multi-user-design.md](multi-user-design.md) の Phase A(認証基盤)を実装済み。
利用者は引き続き実質シングルユーザーだが(users は 1 人のみ、admin 画面や招待制は
Phase C まで未実装)、外部公開に耐える認証を備える:

- `users` / `sessions` / `api_tokens` テーブル(`internal/store/migrations/0005_auth.sql`)。
  パスワードは argon2id(`internal/auth`)でハッシュ化して保存。
- セッションは `crypto/rand` 由来の 256bit トークンを HttpOnly/SameSite=Lax な
  Cookie で発行し、DB には SHA-256 ハッシュのみ保存する。idle 30 日 + absolute 90 日
  で失効し、期限切れセッションは日次メンテナンスジョブ(`internal/maintenance`)で削除。
- 初回起動時(または認証導入前の DB を開いた直後)は、`users` の bootstrap admin が
  ロック状態(`password_hash = '!locked!'`)になっており、初期セットアップ画面
  (`POST /api/v1/auth/setup`)でパスワードを設定するまでログインできない。
  `feedla create-admin` のような対話 CLI は用意していない(Docker イメージが
  `FROM scratch` で対話シェルに向かないため)。
- ログインはアカウント単位の指数バックオフ + IP 単位の固定ウィンドウでレート制限
  (`internal/auth.LoginLimiter`)。存在しないユーザー名でもダミーの argon2id
  検証を行い、応答時間・メッセージを揃えてユーザー列挙を防ぐ。
- Fastladder 互換クライアント等の非ブラウザクライアント向けに `api_tokens`
  (`Authorization: Bearer` または互換の `ApiKey` パラメータ)を用意。
- `/api/v1/auth/login`・`/api/v1/auth/setup`・`GET /healthz`・
  `GET /api/v1/auth/me` 以外の全エンドポイントはデフォルトで認証必須
  (`internal/api/auth_middleware.go`、opt-out 方式)。
- 状態変更 API(GET/HEAD/OPTIONS 以外)は、セッション Cookie で認証されたリクエストに
  限り `Origin` ヘッダの一致を必須にする(欠落も 403)。API トークンで認証された
  リクエストは Cookie を見ないため CSRF 対象外。旧 `internal/api/csrf.go` の
  `checkOrigin`(Origin 欠落を素通しする軽量版)は置き換えられ削除済み。詳細は
  [multi-user-design.md](multi-user-design.md) と
  [security-review-2026-08.md](security-review-2026-08.md) を参照。
- シングルユーザーなので、既定は**リッスンを 127.0.0.1 に限定**(`FR_LISTEN`)する
  運用を引き続き推奨(認証の追加は多層防御であり、ネットワーク限定の代替ではない)。
  Docker イメージのコンテナ内バインドが `0.0.0.0` である理由・ホスト側の
  ポートマッピングでの絞り込み方は変更なし(README.md のクイックスタート参照)。

### デフォルト購読

`internal/feed/seed.opml`（`go:embed`でバイナリに同梱）に登録した feed は、
`feedla serve` 起動時に `feeds` テーブルが空の場合だけ自動 import される
（`internal/feed/seed.go` の `SeedIfEmpty`）。新しい volume/DB でデプロイし
直しても毎回同じ初期購読から始まる。既に 1 件でも feed が存在すれば no-op
なので、seed した feed を後から解除しても再度湧いて出ることはない。増やし
たい場合は `seed.opml` を編集してイメージを作り直す。

## 運用

### 観測

- `slog` (JSON) でクロール結果を 1 行 1 フィード出力（status, 所要時間, 新規件数）。
- `/metrics` (Prometheus 形式、任意有効化):
  - `fetch_total{status}`, `fetch_duration_seconds` (histogram)
  - `feeds_total`, `feeds_erroring`, `entries_unread`
  - `queue_depth`, `db_size_bytes`
- UI に「エラー中のフィード一覧」画面を作る。放置されたリンク切れの掃除は
  この種のツールで最も必要なメンテ作業。

### バックアップ

- 日次で `VACUUM INTO '<FR_BACKUP_DIR>/feedla-YYYYMMDD.db'`（WAL 中でも安全。
  `internal/maintenance/`・`internal/store/backup.go` で実装済み）。タイマーは
  `Runner.Run` 開始(≒ `feedla serve` 起動)から `Interval`(既定 24h)おきの
  `time.Ticker` で、固定時刻には揃わない。`Run` はループに入る前に一度
  `backupIfMissingToday` を呼び、当日分の `feedla-YYYYMMDD.db` が
  `FR_BACKUP_DIR` に無ければ即座に 1 回バックアップする(再起動を挟んで
  ティッカーがリセットされても、その日の分が長時間欠けたままにならない
  ようにするため)。
- OPML エクスポートも日次で吐いておくと、最悪 DB を捨てて再構築できる。
- `FR_BACKUP_REMOTE_*` を設定すると、上記のローカル日次バックアップを
  S3 互換オブジェクトストレージ(さくらのクラウド オブジェクトストレージ想定)
  へミラーする(`internal/remotebackup/` で実装、`aws-sdk-go-v2` の S3 クライアント
  を使用)。ホスト自体を失うケースに対する保険で、アップロード後に世代数
  (デフォルト 5、`.db`/`.opml` それぞれ独立にカウント)を超えた古いオブジェクトを
  自動削除する。S3 プロトコルのテストは実際の AWS には繋がず `gofakes3`
  (in-memory な fake S3 サーバ)を使う。
- `feedla serve` 起動時、`FR_DB_PATH` の DB ファイルが存在しなければ
  `internal/restore/` が自動復旧を試みる。`FR_BACKUP_DIR` 内の最新の
  `feedla-YYYYMMDD.db` を優先し、なければ(`FR_BACKUP_REMOTE_*` 設定時)
  リモートの最新オブジェクトをダウンロードする。どちらにもなければ
  従来どおり空の DB で起動する(エラーにはしない)。既存 DB がある場合は
  一切手を出さない。
- `GET /api/v1/admin/backups`(admin 限定)で、ローカル(`FR_BACKUP_DIR` 配下の
  `feedla-*.{db,opml}`)・リモート(`remotebackup.Client.List` で bucket 全体を
  列挙)それぞれの実在するバックアップファイル一覧(ファイル名・サイズ・
  更新日時)を返す。Web UI の「ユーザー管理」画面から確認できる
  (`AdminOverlay.tsx`)。バックアップの取得自体(`internal/maintenance`)とは
  独立した読み取り専用の確認用エンドポイント。

## リソース見積り

前提: 500 フィード、平均 1 日 5 記事、30 日保持 = 約 75,000 記事。

| 項目 | 見積り |
|---|---|
| DB サイズ | 記事平均 4KB → 約 300MB（+ FTS trigram で 1.5〜2 倍。trigram はインデックスが大きい点に注意） |
| 常駐 RSS | 30〜60MB（Go ランタイム + SQLite cache 20MB + fetch バッファ） |
| ピーク時 | 同時 32 fetch × 数百 KB = +20MB 程度 |
| CPU | 平常時ほぼ 0。クロール tick 時に短時間スパイク |
| ネットワーク | 条件付き GET が効けば 1 日あたり数十 MB |

> FTS の trigram インデックスが重い場合は、`body` を対象から外して `title` のみ、
> あるいは検索対象を直近 N 日に限定する案を検討する。

## ディレクトリ構成

```
cmd/feedla/main.go
internal/
  config/          設定ロード（FR_* 環境変数）
  auth/            argon2id パスワードハッシュ・セッション/APIトークン生成・ログインレート制限
  store/           SQLite アクセス（手書き database/sql、sqlc は不採用）
    migrations/    *.sql（embed、自前マイグレーションランナー）
  crawler/
    scheduler.go
    crawler.go     fetch/parse/write を goroutine 単位で実行
    fetcher.go
    dialer.go      SSRF 対策 dialer
    hostsem.go
    parser.go
    backoff.go
  feed/            自動検出（discover.go）・OPML import/export（pagewatch は除外）
  extract/         抽出パイプラインの共通型（Extractor/Input/Result）
    pagewatch/     フィードのないページの単一ページ監視（DB/HTTP 非依存）
  api/             HTTP ハンドラ（互換 API / v1 API / metrics / stats）
  maintenance/     GC・リテンション・バックアップの日次ジョブ
  metrics/         手書き Prometheus exposition format
  web/             embed した SPA アセット
web/               フロントエンドのソース（Vite）
```

`internal/web/dist`（Vite のビルド出力）は `go:embed` の対象だが git 管理はしない
（`.gitignore` 済み）。そのため **`go build`/`go test` の前に必ずフロントエンドを
ビルドしておく必要がある**。`make build` が `web/` の `pnpm install && pnpm run build`
と `go build ./...` をまとめて実行する。フロントエンドを触りながら開発する場合は
`make web-dev`（Vite dev server）を使う。

## テスト方針

- **パーサ**: RSS 1.0/2.0, Atom, JSON Feed, 壊れた XML, Shift_JIS/EUC-JP など
  各形式の golden ファイルでテーブルドリブンテスト。
  **フィクスチャは第三者のコンテンツを含めない。** 実在フィードをそのまま
  リポジトリに置くと再配布になるため、`example.com` を使った合成データ
  （`internal/crawler/parser_test.go` の書き方）か、実物から構造だけを抽出して
  本文を伏せた匿名化フィクスチャを使う。後者の作り方は
  [方式 A 詳細設計 §14.5](feedless-site-subscription-pagewatch.md#145-構造抽出ツール-toolshtmlskeleton) を参照。
- **クローラ**: `httptest.Server` で 200/304/301/410/429/タイムアウト/巨大ボディ/
  無限リダイレクトを再現。
- **ストア**: 一時ファイル DB で実 SQLite を使う（モックしない）。
- **Fuzz**: パーサに `go test -fuzz` を当てる。
- **並行性**: `-race` を CI で常時有効化。
- **e2e（Web UI）**: `e2e/`（Playwright）が実際の `feedla` API・クローラ・SPA を
  ブラウザ経由で黒箱テストする。SSRF 対策の dialer はループバック上のフィクス
  チャフィードサーバへの接続も拒否してしまうため、`e2e/testserver`（本番の
  `feedla serve` とは別バイナリ、dialer だけ差し替え）を対象に実行する。
  `make e2e` で実行（`build` に依存し、pre-commit には含めない — ブラウザの
  ダウンロードが必要で重いため）。

## 開発フェーズ

| Phase | 内容 | 完了条件 |
|---|---|---|
| 0 | スキーマ + マイグレーション + store | OPML を import して feeds に入る |
| 1 | クローラ（fetch/parse/write, 固定間隔） | CLI で 1 回クロールして記事が入る |
| 2 | スケジューラ・適応間隔・バックオフ・host semaphore | 常駐して安定巡回する |
| 3 | API（v1 + LDR 互換） | curl で購読・未読取得・既読ができる |
| 4 | Web UI（3 ペイン + キーボード + 先読み） | 実用開始（dogfooding）— 完了 |
| 5 | 検索・pin・OPML export・エラー画面 | Fastladder 相当の機能パリティ |
| 6 | メトリクス・GC・バックアップ・コンテナ運用 | 運用に載る |

## 未決事項 / 今後の検討

1. **フィード内の全文取得（本文抜粋しか流さないフィードへの対応）** —
   実装済み。`internal/fulltext`（feedless/`internal/extract` とは無関係の
   独立パッケージ）が entry の link 先ページを Readability 相当で抽出し、
   `feed_fulltext` テーブルで feed 単位に有効/無効を切り替える。購読時に
   候補一覧へ「(本文抽出あり)」を追加、購読後は FeedDetailOverlay の
   本文抽出設定パネルで切り替え可能。
2. **お気に入り記事のアーカイブ**（pin した記事の本文を永続保存するか）。
3. **日本語検索の精度**。trigram で不足なら kagome + bleve への移行を検討。
4. **既読の同期先**。将来的にモバイルから読む場合、Fever API や Google Reader API 互換層を
   追加すると既存のモバイルクライアント（Reeder 等）が使えるようになる。
5. **フィード共有 / 公開**（`subscriptions.is_public` を用意はしたが、当面は使わない）。
