# feedla

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

- マルチテナント / 一般公開向けアカウント管理
- クラスタリング・水平スケール
- ソーシャル機能（共有、コメント、フォロー）
- モバイルネイティブアプリ（Web UI をレスポンシブにする程度に留める）

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
| マイグレーション | `dbmate`（既存の慣れ）または `golang-migrate` の埋め込みモード | SQL ファイルを `embed` して起動時に自動適用。 |
| フィードパーサ | `mmcdole/gofeed` | RSS 0.9x/1.0/2.0, Atom 0.3/1.0, JSON Feed を一括対応。自前実装は互換性地獄なので避ける。 |
| 文字コード | `golang.org/x/net/html/charset` | XML 宣言 / HTTP ヘッダ / BOM から判定。 |
| HTML サニタイズ | `microcosm-cc/bluemonday` | 本文表示時の XSS 対策。 |
| ログ | 標準 `log/slog` (JSON) | 依存ゼロ。 |
| 設定 | 環境変数 + TOML（任意） | 自分専用なので簡素に。 |
| フロントエンド | TypeScript + Preact + Vite（あるいは素の TS） | 軽量。React でも可だがバンドルサイズを抑えたい。 |

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

```
 Scheduler        Fetch Queue        Fetcher Pool        Parse           Writer
 (30s tick)   ──▶ chan *FetchJob ──▶ N goroutines  ──▶ goroutine  ──▶ 1 goroutine
 due な feed を    (buffered, cap=N*4)  HTTP GET        XML/JSON 解析     batch tx
 next_fetch_at
 順に取得
```

各ステージを分離することで、
「HTTP は遅いので並列度を上げたい／パースは CPU バウンドなので NumCPU 程度／
書き込みは SQLite の制約で 1 本」という異なる並列度を自然に表現できる。

### Scheduler

```go
type Scheduler struct {
    store    *Store
    jobs     chan<- *FetchJob
    interval time.Duration // 30s
}
```

- 30 秒ごとに `SELECT ... FROM feeds WHERE next_fetch_at <= ? ORDER BY next_fetch_at LIMIT 200`。
- enqueue した feed は即座に `next_fetch_at = now + interval` に更新して**二重投入を防ぐ**
  （メモリ上の in-flight set も併用）。
- 起動直後に全フィードが一斉に due になるのを避けるため、
  初回登録時とマイグレーション時に `next_fetch_at` へ **ジッタ（0〜interval のランダム）** を入れる。

### 取得間隔の適応制御

LDR 同様、更新頻度に応じて巡回間隔を変える。

```
成功 & 新規記事あり : interval = max(MIN, interval * 0.7)
成功 & 新規記事なし : interval = min(MAX, interval * 1.3)
304 Not Modified   : interval = min(MAX, interval * 1.3)   (コストは低いので緩やかに)
エラー             : interval = min(MAX_ERR, base * 2^min(error_count, 8)) + jitter
```

- `MIN = 10min`, `MAX = 12h`, `MAX_ERR = 24h`
- `429` / `503` は **`Retry-After` を最優先で尊重**。
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

- `gofeed.Parser` に `io.Reader` を渡す。charset は `charset.NewReaderLabel` で吸収。
- 正規化処理:
  - 相対 URL を feed の base URL で絶対化。
  - `published` が無い / 未来 / 極端に古い場合の補正（未来 → now、無し → 初回取得時刻）。
  - 本文は `content:encoded` > `content` > `description` > `summary` の優先順で採用。
  - `bluemonday.UGCPolicy()` ベースのサニタイズ（`script`, `iframe`, `on*`, `style` 除去。
    `img` は `src`/`alt` のみ許可し `loading="lazy"` を付与）。
- 1 フィードあたりの取り込み上限 1000 件（DoS 対策）。
- **パースは worker pool 内で行い、結果は構造体スライスにして Writer へ渡す**。
  巨大フィードでもストリーミングで扱えるよう、上限件数に達したら打ち切る。

### Writer

- チャネルから受け取り、**フィード単位で 1 トランザクション**。
- `INSERT ... ON CONFLICT(feed_id, guid) DO UPDATE` で更新記事にも対応。
  ただし**既読済み記事は body 更新しても未読に戻さない**（LDR の挙動に合わせる）。
- 同一 tx 内で `feeds` のメタ情報と `subscriptions.unread_count` も更新。
- 新規記事 0 件のときは `feeds` の 1 行 UPDATE のみ（最頻ケースを最軽量に）。

### GC / リテンション

日次のバックグラウンドジョブ:

- 既読かつ pin されていない記事のうち、`fetched_at < now - 30日` を削除。
- フィードあたりの保持件数上限（既定 1000 件）を超える古い既読記事を削除。
- `PRAGMA optimize` を実行。週次で `VACUUM INTO` でバックアップ兼断片化解消。

## API 設計

自分専用なので認証は簡素にするが、**LDR 互換 API を用意しておくと既存クライアント資産が使える**ため、
`/api/*` は Fastladder 互換、新規機能は `/api/v1/*` に置く二段構えとする。

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
GET    /healthz  /  /metrics
```

- ページングは `(published_at, id)` の複合カーソル（offset は使わない）。
- 既読送信は**クライアント側でデバウンスしてバルク POST**（1 記事 1 リクエストにしない）。

### フィード自動検出（subscribe 時）

1. 与えられた URL を取得。
2. Content-Type / 中身がフィードならそのまま採用。
3. HTML なら `<link rel="alternate" type="application/rss+xml|atom+xml|feed+json">` を抽出。
4. 候補が複数なら UI に選択肢を返す（`202` + 候補リスト）。
5. それも無ければ `/feed`, `/rss`, `/atom.xml`, `/index.xml`, `/feed.xml` を順に試行。

## Web UI

### レイアウト（LDR 風 3 ペイン）

```
┌──────────────┬─────────────────────────────────────────────┐
│ フォルダ /   │  [フィードタイトル]  未読 32   ★★★  ⟳  ✕   │ ← ヘッダ
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

### キーボードショートカット

| キー | 動作 |
|---|---|
| `j` / `k` | 次 / 前の記事へスクロール（記事単位でスナップ） |
| `space` / `shift+space` | ページ単位スクロール |
| `s` / `a` | 次 / 前の購読へ |
| `v` | 記事を新規タブで開く |
| `p` | pin する |
| `o` | pin 一覧を開く |
| `r` | 未読を再取得(サーバへ再クロールを指示) |
| `/` | 検索 |
| `?` | ヘルプ |

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
  `default-src 'self'; img-src 'self' https: data:; script-src 'self'; frame-src 'none'`
- 外部画像はデフォルトで直接読み込む（自分専用のため）。
  トラッキングが気になる場合のオプションとして**画像プロキシ**を用意（`/img?url=...`、
  署名付き、サイズ上限、private IP 拒否）。

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

- シングルユーザーなので、既定は**リッスンを 127.0.0.1 に限定**。
- 外部公開する場合のために、単一ユーザーのパスワード認証（bcrypt/argon2id）+
  HttpOnly/SameSite=Lax な session cookie を用意。リバースプロキシ側で TLS 終端。
- 状態変更 API は `Origin` ヘッダ検査 + SameSite cookie で CSRF 対策。

## 運用

### 設定（環境変数）

```
FR_LISTEN=127.0.0.1:8080
FR_DB_PATH=/var/lib/feedreader/feedreader.db
FR_FETCH_CONCURRENCY=32
FR_FETCH_MIN_INTERVAL=10m
FR_FETCH_MAX_INTERVAL=12h
FR_RETENTION_DAYS=30
FR_RETENTION_PER_FEED=1000
FR_USER_AGENT="feedreader/0.1 (+https://example.com/bot)"
FR_LOG_LEVEL=info
```

### 観測

- `slog` (JSON) でクロール結果を 1 行 1 フィード出力（status, 所要時間, 新規件数）。
- `/metrics` (Prometheus 形式、任意有効化):
  - `fetch_total{status}`, `fetch_duration_seconds` (histogram)
  - `feeds_total`, `feeds_erroring`, `entries_unread`
  - `queue_depth`, `db_size_bytes`
- UI に「エラー中のフィード一覧」画面を作る。放置されたリンク切れの掃除は
  この種のツールで最も必要なメンテ作業。

### バックアップ

- 日次で `VACUUM INTO '/backup/feedreader-YYYYMMDD.db'`（WAL 中でも安全）。
- OPML エクスポートも日次で吐いておくと、最悪 DB を捨てて再構築できる。

### デプロイ

- 単一バイナリ + systemd unit。`DynamicUser=yes`, `ProtectSystem=strict`,
  `StateDirectory=feedreader`, `NoNewPrivileges=yes`。
- コンテナ版は `FROM scratch` + CA 証明書のみ（pure Go SQLite なので実現できる）。

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

## ディレクトリ構成（案）

```
cmd/feedreader/main.go
internal/
  config/          設定ロード
  store/           SQLite アクセス（sqlc または手書き）
    migrations/    *.sql（embed）
  crawler/
    scheduler.go
    fetcher.go
    hostsem.go
    parser.go
    writer.go
    backoff.go
  feed/            自動検出・正規化・サニタイズ
  api/             HTTP ハンドラ（互換 API / v1 API）
  web/             embed した SPA アセット
web/               フロントエンドのソース（Vite）
```

- クエリは `sqlc` でコード生成すると型安全でよいが、SQLite + FTS の
  動的クエリが増えるようなら手書き + `database/sql` で十分。

`internal/web/dist`（Vite のビルド出力）は `go:embed` の対象だが git 管理はしない
（`.gitignore` 済み）。そのため **`go build`/`go test` の前に必ずフロントエンドを
ビルドしておく必要がある**。`make build` が `web/` の `pnpm install && pnpm run build`
と `go build ./...` をまとめて実行する。フロントエンドを触りながら開発する場合は
`make web-dev`（Vite dev server）を使う。

## テスト方針

- **パーサ**: 実在フィードの golden ファイル（RSS 1.0/2.0, Atom, JSON Feed, 壊れた XML,
  Shift_JIS/EUC-JP のフィード）をリポジトリに置いてテーブルドリブンテスト。
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
| 6 | メトリクス・GC・バックアップ・systemd | 運用に載る |

## 未決事項 / 今後の検討

1. **フィード内の全文取得（本文抜粋しか流さないフィードへの対応）**
   readability 相当の本文抽出をやるか。やる場合は取得負荷とサイト側への礼儀に注意。
2. **お気に入り記事のアーカイブ**（pin した記事の本文を永続保存するか）。
3. **日本語検索の精度**。trigram で不足なら kagome + bleve への移行を検討。
4. **既読の同期先**。将来的にモバイルから読む場合、Fever API や Google Reader API 互換層を
   追加すると既存のモバイルクライアント（Reeder 等）が使えるようになる。
5. **Rate limit 情報の永続化**。`Retry-After` をプロセス再起動をまたいで保持するか。
6. **フィード共有 / 公開**（`subscriptions.is_public` を用意はしたが、当面は使わない）。
