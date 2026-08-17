# feedla

livedoor Reader / Fastladder のような「大量のフィードを高速に読み流す」体験を
再現した、自分専用のセルフホスト型 RSS/Atom リーダーです。単一バイナリ + SQLite
だけで動き、外部 DB やジョブキューは不要です。

## スクリーンショット

| デスクトップ(3 ペイン) | モバイル: 記事ペイン | モバイル: 購読一覧 |
|---|---|---|
| ![デスクトップ](docs/screenshots/desktop.png) | ![モバイル: 記事ペイン](docs/screenshots/mobile-entries.png) | ![モバイル: 購読一覧](docs/screenshots/mobile-sidebar.png) |

## 特徴

- **シングルバイナリ + SQLite 1 ファイル**。外部ミドルウェアなしで動く。
- **軽量**。数百〜千フィード規模を RAM 100MB 以下・常時 CPU ほぼゼロで運用できる。
- **LDR 風の 3 ペイン UI**。キーボードだけで大量の記事を読み流せる。
- **スマホ対応**。狭幅では 1 画面ずつの表示に切り替わり、スクロールで既読になる。
  左右スワイプで次 / 前の記事へ移動でき、長い記事を最後までスクロールせずに読み飛ばせる。
- **フィード取得は並列・適応間隔**。更新頻度に応じて巡回間隔を自動調整し、
  取得元サーバへの負荷にも配慮する（条件付き GET、ホストごとの同時接続制限など）。
- OPML のインポート / エクスポート、全文検索、あとで読む(pin)、フォルダ分けに対応。
- Docker イメージは非 root・`FROM scratch` ベースで実測 約13MB。

利用者は自分専用（シングルユーザー）を前提とした設計です。内部設計・アーキテクチャの
詳細は [docs/DESIGN.md](docs/DESIGN.md) を参照してください。

## クイックスタート（Docker）

```sh
docker run -d \
  --name feedla \
  -p 127.0.0.1:8080:8080 \
  -v feedla-data:/data \
  ghcr.io/tokuhirom/feedla:latest
```

起動後、ブラウザで `http://localhost:8080` を開きます。データは `/data`
（コンテナ内）に永続化されるので、上記のように named volume を割り当ててください。

初回起動時は「初期セットアップ」画面が表示されるので、管理者アカウントの
ユーザー名とパスワード(12文字以上)を設定してください。以後はログイン画面が
表示されます。既存の(認証導入前の)DB を新しいバイナリで開いた場合も、
既存データはそのまま保持されたうえで同じセットアップ画面が一度だけ表示されます
(パスワードが未設定の間はログインできません)。

初回起動時、`feeds` テーブルが空であればデフォルトの購読リストが自動投入されます。

`-p 127.0.0.1:8080:8080` によりホスト側もループバックに限定しています。
セッション Cookie + パスワード認証を備えているため LAN 内公開程度であればこのままでも
安全ですが、インターネットに公開する場合は HTTPS 終端(リバースプロキシ等)を
挟み、`FR_COOKIE_SECURE=true` を設定してください（詳細は後述の「設定」参照）。

## ソースからのビルド

Go 1.26+ / Node.js（`pnpm`）が必要です。

```sh
make build   # フロントエンドのビルド → Go バイナリのビルド
./feedla serve
```

開発時にフロントエンドを触る場合は `make web-dev`（Vite dev server）を使います。

## 設定（環境変数）

| 変数 | デフォルト | 説明 |
|---|---|---|
| `FR_LISTEN`(または `feedla serve --listen`) | `127.0.0.1:8080`（Docker イメージでは `0.0.0.0:8080`） | コンテナ**内**の待受アドレス。`--listen` を指定すると `FR_LISTEN` より優先される。Docker の `-p` によるポートマッピングを機能させるため、コンテナ内は `0.0.0.0` のまま変更しない。ホスト側の公開範囲は `docker run -p <ホストIP>:8080:8080` の `<ホストIP>` 側で制御する(クイックスタートは `127.0.0.1` 限定がデフォルト)。 |
| `FR_DB_PATH` | `/var/lib/feedla/feedla.db`（Docker イメージでは `/data/feedla.db`） | SQLite ファイルのパス。 |
| `FR_FETCH_CONCURRENCY` | `32` | フィード取得の全体同時実行数。 |
| `FR_FETCH_MIN_INTERVAL` | `10m` | 巡回間隔の下限。 |
| `FR_FETCH_MAX_INTERVAL` | `12h` | 巡回間隔の上限。 |
| `FR_RETENTION_DAYS` | `30` | 既読記事を保持する日数。 |
| `FR_RETENTION_PER_FEED` | `1000` | フィードごとの記事保持件数上限。 |
| `FR_BACKUP_DIR` | (未設定、日次バックアップ無効) | 日次バックアップ（`VACUUM INTO`）の出力先。設定した場合のみ有効になる。 |
| `FR_USER_AGENT` | `feedla/0.1 (+https://example.com/bot)` | フィード取得時の User-Agent。 |
| `FR_LOG_LEVEL` | `info` | ログレベル。 |
| `FR_COOKIE_SECURE` | `auto` | セッション Cookie の `Secure` 属性(`auto`/`true`/`false`)。`auto` はそのプロセスが直接 TLS を終端している場合のみ `Secure` を付ける(`X-Forwarded-Proto` は信用しない)。リバースプロキシで TLS 終端する構成では明示的に `true` を指定すること。 |
| `FR_PUBLIC_ORIGIN` | (未設定、`Host` ヘッダにフォールバック) | CSRF 対策の Origin 検証で期待する Origin。`Host` ヘッダを書き換えるリバースプロキシ配下で必要な場合に設定する。例: `https://feedla.example.com`。 |
| `FR_METRICS_TOKEN` | (未設定) | `GET /metrics` をセッション Cookie なしで叩きたい監視系向けの Bearer トークン。未設定時は `/metrics` も通常のログインと同様に認証が必須。 |
| `FR_QUOTA_MAX_SUBSCRIPTIONS` | `2000` | ユーザーごとの購読数上限。超過時は購読追加を 400 で拒否。 |
| `FR_QUOTA_MAX_SCRAPE_SOURCES` | `50` | ユーザーごとの pagewatch(scrape_sources)作成数上限。 |
| `FR_QUOTA_MAX_PINS` | `10000` | ユーザーごとの pin 数上限。 |
| `FR_QUOTA_MAX_IGNORE_WORDS` | `1000` | ユーザーごとの無視ワード数上限。 |
| `FR_QUOTA_OPML_MAX_FEEDS` | `2000` | OPML import 1 回あたりのフィード件数上限。超過時は import 全体を拒否(部分適用しない)。 |
| `FR_QUOTA_FEED_ADD_PER_HOUR` | `60` | ユーザーごとのフィード追加(購読 + discover)のレート制限(回/時)。超過時は 429。 |
| `FR_QUOTA_REFRESH_PER_HOUR` | `30` | ユーザーごとの手動 refresh のレート制限(回/時)。超過時は 429。 |
| `FR_QUOTA_PREVIEW_PER_HOUR` | `30` | ユーザーごとの pagewatch preview のレート制限(回/時)。超過時は 429。 |
| `FR_QUOTA_API_PER_MINUTE` | `600` | ユーザーごとの API 全体リクエスト数の粗いレート制限(回/分)。超過時は 429。 |

いずれの `FR_QUOTA_*` も `0` 以下を指定するとその項目の制限を無効化する。

feedla は認証必須(パスワード + セッション Cookie)です。初回起動時にセットアップ
画面で管理者アカウントを作成してください(詳細は前述の「クイックスタート」参照)。
バイナリを直接実行する場合も、`FR_LISTEN` を信頼できるネットワークに限定するのが
基本方針です(デフォルトの `127.0.0.1:8080` のままなら安全)。Docker で外部公開
したい場合は、`FR_LISTEN` ではなく `docker run -p` のホストIP側を変更するか
(例: `-p 0.0.0.0:8080:8080`)、リバースプロキシ側で HTTPS 終端したうえで
そちらを公開してください(その場合は `FR_COOKIE_SECURE=true` を忘れずに)。

## キーボードショートカット

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

## バックアップ

`FR_BACKUP_DIR` を設定していれば、日次で `VACUUM INTO '<FR_BACKUP_DIR>/feedla-YYYYMMDD.db'`
を実行し、WAL 中でも安全にバックアップを取得します。OPML エクスポート
（`GET /api/v1/opml`）も定期的に取得しておくと、最悪 DB を失っても購読リストから
復元できます。

スキーマ変更を伴うアップデートの前など、任意のタイミングで手動バックアップを
取りたい場合は `feedla backup <dest>` を使ってください。稼働中の `feedla serve`
と同じ DB ファイルに対して別プロセスから安全に実行できます（`VACUUM INTO` は
WAL 中・同時アクセス中でも一貫性のあるスナップショットを取れる）。

```sh
docker exec feedla feedla backup /data/pre-upgrade.db
docker cp feedla:/data/pre-upgrade.db ./pre-upgrade.db
```

## 開発に参加する

開発運用（ブランチ運用・pre-commit・ツールチェーン管理など）は
[CLAUDE.md](CLAUDE.md) を、内部設計・アーキテクチャ・API 仕様・テスト方針などは
[docs/DESIGN.md](docs/DESIGN.md) を参照してください。
