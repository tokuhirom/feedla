# セキュリティレビュー(2026-08)

2026-08-16 に実施した、feedla の設計(`docs/DESIGN.md`)と実装コードの突き合わせによる
セキュリティレビューの結果。認証・認可、SSRF、XSS/サニタイズ、インジェクション、
デプロイ構成、依存関係の各観点で調査した。

総じて設計思想は堅実(SQL は全プレースホルダ化、FTS5 はフレーズエスケープ、
専用 SSRF dialer、bluemonday による二重サニタイズ、Docker は非 root + `scratch`)
だが、**「シングルユーザー・127.0.0.1 限定」という設計前提と、実際の Docker 配布の
デフォルト挙動に食い違いがある**のが最大の懸念。

## 🔴 High

### 1. Docker のデフォルト構成が「認証なし・127.0.0.1 限定」という設計前提を崩している

> **対応済み(2026-08)**: README のクイックスタートを `-p 127.0.0.1:8080:8080`
> （ホスト側もループバック限定）に変更した。コンテナ内の `FR_LISTEN=0.0.0.0:8080`
> は Docker のポートマッピングを機能させるために必須のため変更していない
> （変更するとポートマッピング自体が動作しなくなる）。詳細は `docs/DESIGN.md`
> の「認証」節、`Dockerfile`/`Dockerfile.goreleaser` のコメントを参照。

- `Dockerfile:32` で `ENV FR_LISTEN=0.0.0.0:8080`(コンテナ内は全 interface にバインド)。
- `README.md` のクイックスタートが `docker run -p 8080:8080 ...` をそのまま案内しており、
  これはホストの全 interface に公開される。
- 一方 `internal/api/api.go` には認証・セッション・CSRF 検証が一切なく、
  `docs/DESIGN.md` の「認証」節は「既定はリッスンを 127.0.0.1 に限定。現状の実装は
  これのみで運用している」と断定している。
- → **README 通りに Docker で動かすと、LAN/インターネットに直接繋がったホストでは
  無認証 API が即座に外部到達可能になる。** リバースプロキシでの認証が必須である旨を
  クイックスタート本文に明記すべき。

## 🟡 Medium

### 2. 状態変更エンドポイントが素朴な CSRF で成立する

- `internal/api/ldr.go` の `/api/subscribe`, `/api/unsubscribe`, `/api/pin/add` は
  `form-urlencoded` の単純 POST で発火可能。悪意あるページに置いた自動送信フォームで、
  被害者のブラウザ経由(localhost 到達可能な環境なら)で購読の追加・解除ができてしまう。
- `/api/v1/subscriptions`, `/api/v1/opml`, `/api/v1/pins` など JSON POST 系は
  Content-Type を検証しておらず、`text/plain` フォームを使った JSON-CSRF 手法が成立する。
- CORS ヘッダ自体は一切設定されていない(懸念なし)ため、`fetch()` 経由の攻撃は
  preflight で弾かれる。

### 3. SSRF 対策 dialer に CGNAT レンジ(100.64.0.0/10)のブロック漏れ

> **対応済み(2026-08)**: `internal/crawler/dialer.go` に `isCGNAT` を実装し
> `isBlockedIP` に組み込んだ。`100.64.0.0/10` の境界値・Alibaba Cloud メタデータ
> エンドポイント(`100.100.100.200`)を含むテストケースを追加済み。
>
> あわせて、`net.IP.To4()` が展開しない IPv4 埋め込み IPv6 アドレス
> (NAT64 well-known prefix `64:ff9b::/96`、6to4 `2002::/16`、廃止済みの
> IPv4互換形式 `::a.b.c.d`)も `embeddedIPv4` で検出し、埋め込まれた
> IPv4 アドレスに対して再帰的に同じチェックをかけるようにした。

- `internal/crawler/dialer.go` の `isBlockedIP` に、設計書が言及する `isCGNAT` が
  実装されていない。
- 実測で `100.100.100.200`(Alibaba Cloud のメタデータエンドポイント)がブロックされない
  ことを確認。DNS rebinding・悪意あるリダイレクト先としてこのレンジが使われると
  SSRF 対策を迂回できる。

### 4. Go ツールチェーンが既知脆弱性のあるバージョンに固定

- `mise.toml`/`go.mod` が go1.26.4 に固定されており、`govulncheck` で以下が
  実コードから到達可能と確認:
  - `encoding/xml`(GO-2026-6088、再帰深度ガード欠如)→ OPML/フィードパース
    (`internal/feed/opml.go`, `internal/crawler/parser.go`)が該当。設計書の
    「XML 爆弾対策」が前提とする安全性が一部崩れている。
  - `net/http`(GO-2026-6089)、`crypto/tls`(GO-2026-6090)、`net/url`(GO-2026-6218)も
    到達可能。
  - go1.26.6 以上へ更新推奨。

### 5. フィード由来 URL のスキーム検証が皆無

- `internal/crawler/parser.go` の `resolveURL()` はスキーム検証なし。本文
  (bluemonday 経由)は安全だが、`entry.url`/`feeds.site_url` は素通りで保存・描画される
  (`web/src/components/EntryItem.tsx`, `FeedDetailOverlay.tsx` など)。
- 悪意あるフィードが `javascript:` リンクを `<link>` に仕込める。現状は CSP
  `script-src 'self'` によって主要ブラウザでは実行がブロックされているが、CSP 頼みの
  偶発的防御であり、サーバ/フロントいずれでもスキームを `http(s)` に明示制限していない。

## 🟢 Low

- SSRF dialer が `0.0.0.0/8`(`0.0.0.0` 以外)を未ブロック、`safeDialer` の
  `Timeout`/`KeepAlive` が設計書記載値と不一致(`internal/crawler/dialer.go`)。
- `cmd/feedla/main.go` の `http.Server` に `ReadHeaderTimeout` 等の明示設定なし。
- 設計書の記述精度: img 許可ポリシーの実態(`loading="lazy"` 未付与、
  `align`/`height`/`width` も許可)や、Docker の `0.0.0.0` デフォルトへの言及が
  DESIGN.md に反映されていない。

## ✅ 懸念なし

- SQL インジェクション(全プレースホルダ化)
- FTS5 クエリインジェクション(フレーズエスケープ済み)
- XXE(標準 `encoding/xml` のデフォルト安全動作。ただし上記 4 の Go バージョン由来の
  再帰深度問題は別)
- パストラバーサル(ユーザー制御パスが API 経由で存在しない)
- コマンドインジェクション(`os/exec` 不使用)
- pagewatch 経路の本文サニタイズ(通常フィードと同一経路で二重防御)
- `dangerouslySetInnerHTML` の使用範囲(サニタイズ済みの `entry.body` のみ)
- Docker イメージ(`scratch` + 非 root UID + 最小公開ポート)
- ハードコードされたシークレット
- CORS ワイルドカード

## 対応優先順(提案)

1. README/Docker のデフォルト公開設定の是正(High、告知コストのみで着手可能)
2. Go バージョンの更新(Medium、対応コスト最小)
3. `isCGNAT` の実装(Medium)
4. CSRF 対策・URL スキーム検証(Medium、設計・実装コストがやや大きいため別途検討)
