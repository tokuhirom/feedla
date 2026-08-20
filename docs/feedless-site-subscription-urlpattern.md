# 方式 B1.5: 一覧ページからの記事抽出（urlpattern）詳細設計

ステータス: **設計案（未実装）**。
[フィード非提供サイトの購読機能 — 方針検討](feedless-site-subscription.md) の
**Phase F1（方式 B1.5）** の実装設計。全体方針・他方式との比較はそちらを、
先行して実装済みの単一ページ監視は
[方式 A: 単一ページ監視（pagewatch）詳細設計](feedless-site-subscription-pagewatch.md)
（以下「F0 設計」）を参照する。

## 1. スコープ

**やること**: ユーザーが登録した 1 つの**一覧ページ**（ブログのトップ、
お知らせ一覧、ニュースの特集面）を定期取得し、ページ内の `<a>` から
**ユーザーが指定した正規表現にマッチする URL** を記事リンクとして拾い、
初出の URL について**個別ページを取得して本文を抽出**し、
**記事 1 件 = エントリー 1 件**として既存の未読管理に流す。

F0 との違いは「1 URL = 1 エントリー着地点」という制約が外れることで、
方針検討ドキュメントの対象サイト例パターン 1〜3（企業のお知らせ一覧、
フィード配信忘れの個人ブログ、ニュースの特集面）＝**本来の要望の中心**が
ここでカバーされる。

**やらないこと（F1 の非スコープ）**:

- GUI プレビュー上でのクリック選択（方式 B1 / Phase F2）
- CSS セレクタによる一覧範囲の指定（Config に予約枠だけ用意する。§4.2）
- 記事一覧の自動検出・パターンの自動推測（方式 B2 / Phase F3）
- sitemap.xml / JSON-LD からの一覧取得（方式 B0。§13 に持ち越し）
- JS レンダリング（方式 E、恒久的に対象外）
- ページネーションを辿って過去記事を遡ること（§4.4 で「一覧ページ 1 枚のみ」と決める）

**F0 の機能を置き換えるものではない**。1 ページの中身が書き換わるだけの
サイト（ステータスページ、最新 N 件を 1 ページに載せる日記）は引き続き
pagewatch が担当し、urlpattern は「記事ごとに個別 URL を持つサイト」を担当する。
`scrape_sources.kind` で並存する。

## 2. 全体像

```
 Scheduler(既存 30s tick)
   └─ ClaimDueFeeds ──▶ crawlOne(feed)
                          │
                          ├─ feed_url の接頭辞は？
                          │     なし        ─▶ 既存の ParseFeed 経路（変更なし）
                          │     pagewatch:  ─▶ 既存の extractPage 経路（変更なし）
                          │     urlpattern:
                          ▼
                  Fetcher.Fetch(一覧URL, Accept: text/html)  ← 条件付きGETは既存のまま
                          │ 304 → 何もせず interval 更新（最安）
                          ▼ 200
                  charset デコード（既存 DecodeUTF8）
                          │
                          ▼
        internal/extract/urlpattern.Extract(html, prevState)
                  ├─ x/net/html でパース
                  ├─ <a href> を文書順に収集（同一ホスト・スキーム制限）
                  ├─ URL 正規化（絶対化・トラッキングパラメータ除去・フラグメント除去）
                  ├─ url_pattern / exclude_pattern でフィルタ
                  ├─ リンクテキストが空・短すぎるものを除去
                  ├─ 同一 URL を重複排除（先勝ち）
                  └─ prevState.seen に無い URL = 新着候補
                          │
                          ▼
                  *gofeed.Feed（新着候補 N 件。本文は空、Link と Title のみ）
                          │
                          ▼  ← ここから crawler の責務（extract は HTTP を知らない）
              fetchArticles: 新着候補を最大 max_items_per_crawl 件まで
                  ├─ robots.txt 判定（§7.4）
                  ├─ Fetcher.Fetch(記事URL)  ← HostSemaphore で礼儀は既存機構どおり
                  ├─ internal/fulltext.Extract（Readability）
                  └─ 公開日時の抽出（§4.6）
                          │
                          ▼
            既存 crawler.normalizeItem → store.UpsertEntries（無変更）
                          │
                          ▼
            scrape_sources.state を更新（取り込みに成功した URL のみ seen に入れる）
```

**設計の要（F0 から引き継ぐ）**: 抽出結果を `*gofeed.Feed` に落とすことで、
サニタイズ・本文長切り詰め・`EntryInput` 化・`UpsertEntries` という
既存の書き込みパイプラインを一切変更せずに再利用する。

**設計の要（F1 で新たに要るもの）**: 「一覧の解析（純粋関数）」と
「個別ページの取得（HTTP）」を分離し、後者を crawler 側に置く。

## 3. パッケージ構成と依存方向

```
internal/extract/            共通型（Extractor, Input, Result）。DB も HTTP も知らない
  pagewatch/                 方式A（実装済み）
  urlpattern/                方式B1.5（新規）。x/net/html だけに依存
internal/fulltext/           Readability 本文抽出（実装済み）。urlpattern から import しない
internal/crawler/            → internal/extract と internal/fulltext を import
internal/store/              scrape_sources の CRUD。どちらも import しない
```

- **新規の外部依存はゼロ**。`golang.org/x/net/html` と、既に入っている
  `codeberg.org/readeck/go-readability`（`internal/fulltext` 経由）で足りる。
- `internal/extract/urlpattern` は `internal/fulltext` を **import しない**。
  本文抽出は HTTP 取得とセットでしか意味がなく、その HTTP は crawler の
  責務だから。urlpattern は「一覧 HTML → 記事 URL とタイトルの列」だけを
  返す純粋関数に徹する。この線を引くことで、urlpattern の単体テストは
  ネットワークも DB も使わずに書ける（F0 と同じ性質を保つ）。

### 3.1 既存の Extractor インターフェースで足りるか

`extract.Extractor` は `Extract(ctx, Input) (*Result, error)` で、
**HTML 1 枚を受け取って 1 つの疑似フィードを返す**。F1 は 1+N 取得なので
一見合わないが、次のように解釈すれば**インターフェースは変更不要**である。

- `Result.Feed.Items` は「タイトルとリンクだけが埋まった、本文が空の記事」の列。
  これは RSS の世界では珍しくない形（本文を配信しないフィード）で、
  `gofeed.Feed` 中間表現として自然に表現できる。
- 本文の充填は crawler 側の後処理。既に `applyFulltext`（実 feed の
  entry を全文化する処理）が「`ParsedFeed.Entries` を in-place で書き換える」
  という同じ形をしており、**同じ関数を共用できる**（§7.2）。

F0 設計 §13 の申し送り「`Extractor` インターフェースと不透明 `State` が
`urlpattern` 方式でも無理なく使えるかを検証する」に対する回答が、この節である。
**結論: 使える。インターフェースの変更は不要**。ただし `Result` に
「この Item は本文の充填を要する」というフラグは要らない — 後述のとおり
`Config.fulltext` を crawler が読んで判断する方が、extract 側に
HTTP の存在を匂わせずに済む。

## 4. 抽出パイプライン（urlpattern）

### 4.1 なぜ URL パターンなのか（DOM 構造ではなく）

方針検討ドキュメントの B1.5 の主張をそのまま採る。

| | DOM セレクタ（B1/F2） | URL パターン（B1.5・採用） |
|---|---|---|
| 何に依存するか | マークアップ（class 名・入れ子） | **サイト運営者が意図的に決めた URL 設計** |
| 壊れる頻度 | CSS フレームワーク更新・リニューアルで壊れる | URL 設計はリニューアルでも維持されることが多い |
| ユーザーの入力コスト | GUI が要る（作り込みが重い） | 正規表現 1 本（ただし正規表現が書ける前提） |
| 誤爆の方向 | ナビゲーション・広告を巻き込む | **同一ホスト内の記事以外の固定ページ**（`/about` 等）を巻き込む |

URL パターンの誤爆は「関係ないページが 1 回だけエントリーになる」で済み、
かつ `exclude_pattern` で潰せる。DOM セレクタの誤爆は
「一覧そのものが取れなくなる」なので、**壊れ方の質が違う**。

正規表現を書けないユーザーへの手当ては Phase F2（GUI）に譲る。
F1 ではプレビュー（§8.2）で「そのパターンで何件・どのタイトルが拾えるか」を
即座に返すことで、試行錯誤のコストを下げる方向で対処する。

### 4.2 リンクの収集

`x/net/html` で一覧ページをパースし、**文書順**に `<a href>` を集める。

- pagewatch の除去規則（`removeNoise`）は**適用しない**。一覧ページの記事
  リンクは `<nav>` や `<aside>` の中にある場合があり（サイドバーの
  「最新記事」リストなど）、除去すると記事を取りこぼす。F1 のフィルタは
  URL パターンなので、ノイズ除去に頼る必要がそもそもない。
- ただし `<script>` / `<template>` の中身は無視する（パーサが要素として
  扱うものだけを見るため実質自動的にそうなるが、明示的に落とす）。
- `Config.link_scope_selector`（既定 `""`）は**予約枠**として Config に
  用意するが F1 では実装しない。F0 の `scope_selector` と同じ扱いで、
  CSS セレクタのライブラリ（`cascadia` 等）の追加が要るため F2 に譲る。

### 4.3 URL の正規化とフィルタ

収集した `href` を次の順で処理する。

1. **絶対化**: 一覧ページの URL を base に解決する。解決できないもの
   （`javascript:`、`mailto:`、空文字、パースエラー）は捨てる。
2. **スキーム制限**: `http` / `https` 以外は捨てる。
3. **フラグメント除去**: `#section` は落とす。`/a/#c1` と `/a/#c2` が
   別記事として 2 件湧くのを防ぐ。ページ内アンカーだけのリンク
   （解決後に一覧 URL 自身と一致するもの）はここで消える。
4. **トラッキングパラメータ除去**: `utm_*`, `fbclid`, `gclid`, `_ga`,
   `mc_cid`, `mc_eid` を除去する（F0 §4.3 と同じ集合・同じ実装を共有する）。
   これをやらないと、一覧ページが計測パラメータを付け替えるだけで
   同じ記事が毎回「新着」になる。
5. **同一ホスト制限**: `Config.same_host_only`（**既定 true**）が真なら、
   一覧ページとホストが一致しない URL を捨てる。外部リンク（引用先、
   スポンサー、SNS）を拾わないための既定の安全弁。`www.` の有無は
   同一とみなす。ホストをまたぐ CDN 配信のブログを購読したい場合のみ
   false にする。
6. **`url_pattern` によるマッチ**（**必須項目**）: 正規化後の**絶対 URL 全体**に
   対して Go の `regexp` で**部分一致**を取り、マッチしないものを捨てる。
   絶対 URL 全体に当てるので `^https://example\.com/news/` のような
   固定もできるし、`/news/\d{4}/` のような部分指定もできる。
7. **`exclude_pattern`**（任意、既定 `""`）: マッチしたものを捨てる。
   `/news/(index|archive)` のような一覧側リンクの除去に使う。
8. **リンクテキストの検査**: `<a>` 配下のテキストを正規化（F0 §4.4 と
   同じ NFKC → 空白畳み込み）し、`Config.min_title_chars`（既定 2）文字
   未満なら捨てる。「続きを読む」だけのリンクや、画像のみのリンクを
   落とすため。ただし `<a>` にテキストが無く `<img alt>` がある場合は
   alt をタイトル候補として使う。
9. **重複排除**: 同一 URL は**文書順で先に出た方を残す**。一覧では
   「サムネイル画像のリンク」と「タイトルテキストのリンク」が同じ記事を
   指して 2 回現れることが多く、後者を残したいので、**テキストが空でない方を
   優先する**例外を設ける（先勝ちだが、先勝ち側のテキストが空なら
   後から来たテキストで上書きする）。

上限: 1 回の解析で見る `<a>` は **5000 件**、フィルタ通過後の候補は
**500 件**まで（超過分は文書順で切り捨て、`State.truncated` を立てて
UI に警告を出す。F0 のブロック数上限と同じ考え方）。

### 4.4 新着判定 — なぜ「URL の初出」だけで判定するのか

方針検討ドキュメントの未解決論点に「URL の初出だけで十分か、
並び順・日付要素も見るか。並び順が変わるサイトでは誤検知しやすい」とあるが、
**F1 は URL の初出のみで判定する**。理由:

- 「並び順で新着を判定する」方式こそが並び順の変化に弱い。URL の集合差分は
  並び順に**一切影響されない**ので、おすすめ順・人気順に切り替わっても誤検知しない。
- 一覧上の日付表示は形式がばらばら（`2026/08/20`、`8月20日`、`3日前`）で、
  パースの失敗が新着判定の失敗に直結する。日付は「エントリーの
  `published_at` をどう埋めるか」（§4.6）の材料に留め、**新着判定には使わない**。

**ページネーションを辿らない**のもこの判定と対になっている。1 ページ目だけを
見ていれば、新着は必ず 1 ページ目に現れる（新しい記事が 2 ページ目に
直接生えるサイトは考えなくてよい）。過去記事の遡及取得は、
「購読した時点より前の記事は読まない」という RSS 本来の挙動と一致しており、
機能として不足していない。

したがって state が持つべきものは「**これまでに見た記事 URL の集合**」だけになる（§6.1）。

### 4.5 個別ページの取得と本文抽出

新着候補のうち、1 回のクロールで実際に取得するのは
`Config.max_items_per_crawl`（**既定 20**）件まで。文書順（＝多くのサイトで
新しい順）に取る。溢れた分は state に入れない（＝次回のクロールで
また新着候補として現れる）ので、**取りこぼしではなく後回し**になる。

各記事 URL について:

1. `Fetcher.Fetch`（条件付き GET なし。初回取得なので ETag を持っていない）
2. `crawler.DecodeUTF8` で charset デコード
3. `internal/fulltext.Extract`（Readability）で本文抽出
4. 本文のプレーンテキスト長が `minFulltextChars`（既存の 200）未満なら
   **本文なしとして扱う**（ログイン壁・抜粋ページの可能性）
5. `bodyPolicy.Sanitize` + `truncateUTF8`（既存の共通処理）

`Config.fulltext`（既定 **true**）が false の場合は 1〜5 を丸ごと省略し、
**タイトルとリンクだけのエントリー**にする。1 サイト 1 リクエストで済むので
サイトへの負荷は最小になるが、読み流し体験としては弱い。
「サイトに負荷をかけたくない」「本文抽出が壊滅的に失敗する」ときの逃げ道として用意する。

**取得に失敗した記事の扱い**（この機能の要）:

| 失敗 | 扱い |
|---|---|
| fetch 失敗 / 4xx / 5xx | エントリーを**作らない**。state の `seen` にも**入れない**（次回リトライ）。`pending` の失敗回数を +1 |
| 本文抽出失敗 / 短すぎ | エントリーを**タイトル＋リンクのみで作る**。`seen` に入れる（リトライしない） |
| 失敗回数が `maxArticleRetries`（3）に達した | エントリーをタイトル＋リンクのみで作り、`seen` に入れて諦める |

fetch 失敗を `seen` に入れずリトライするのは、**GUID が URL ベース（§5.2）
だから安全にできる**設計。同じ記事を次回また取り込んでも
`UpsertEntries` は UPDATE 経路に入り、重複エントリーにならず、
`newCount` にも数えられない（適応間隔が誤って縮まらない）。
一方で 404 の URL を永久にリトライすると無駄なので、3 回で諦める。

**初回クロールの扱い**: `state` が NULL の初回も、通常どおり
`max_items_per_crawl` 件（既定 20）を取り込む。一覧に 100 件並んでいても
21 件目以降は次回以降に持ち越される。pagewatch のように
「監視を開始しました」の 1 件を作る特別扱いは**しない** — F1 は
初回から本物の記事が並ぶので、空にならないという目的が自然に満たされる。

なお、初回に日付なしの記事が大量に入ると `UpsertEntries` の
バックログ抑止（`DateMissing` なエントリーは先頭 1 件だけ未読）が働き、
未読 1 件になる。これは既存の意図された挙動なのでそのまま乗せる
（§4.6 で日付を埋められれば発生しない）。

### 4.6 公開日時の決定

`published_at` を「取得時刻」にしてしまうと、一覧の下の方にある古い記事を
後から取り込んだときに**時系列が壊れる**（3 年前の記事が今日の記事として並ぶ）。
そこで個別ページから次の順で公開日時を探す。

1. `<meta property="article:published_time">`（OGP。ニュース系で普及率が高い）
2. JSON-LD（`application/ld+json`）の `datePublished`
   （`Article` / `NewsArticle` / `BlogPosting` のいずれか、最初に見つかったもの）
3. `<time datetime="...">` のうち**文書順で最初のもの**
4. いずれも無ければ **取得時刻**とし、`DateMissing` を立てる

パースは RFC3339 → `2006-01-02` の順に試す。未来日（現在時刻 + 1 日より先）は
信用せず取得時刻にフォールバックする（予約投稿のプレースホルダ対策）。

この日付抽出は `internal/extract/urlpattern` ではなく
**`internal/fulltext` 側に `ExtractPublished(html) (time.Time, bool)` として置く**。
理由: 「個別記事ページから記事メタデータを取る」という同じ関心事であり、
DESIGN.md 未決事項 #1 の全文取得機能（実フィードの entry を全文化する経路）でも
「フィードに日付が無いが記事ページには日付がある」ケースで同じものが使えるため。
方式 B0（構造化データの活用）のうち、**費用対効果が明確な部分だけを
ここで先取りする**という位置づけでもある。

### 4.7 タイトルの決定

`Config.title_source`:

| 値 | 挙動 |
|---|---|
| `"list"`（**既定**） | 一覧ページのリンクテキストを使う。空なら記事ページの Readability タイトル → `<title>` の順にフォールバック |
| `"article"` | 記事ページの Readability タイトルを使う。空なら一覧のリンクテキスト |

既定を `"list"` にするのは、一覧のリンクテキストが
**サイト運営者が「一覧で見せたい題」として書いたもの**であり、
`<title>` にありがちな「記事名 | サイト名」のサイト名付きより読み流しに向くため。

## 5. エントリー生成

### 5.1 gofeed.Feed へのマッピング

| gofeed | 値 |
|---|---|
| `Feed.Title` | 一覧ページの `<title>`（空なら URL のホスト名）。F0 の `displayTitle` を共有 |
| `Feed.Link` | 一覧ページの URL |
| `Item.Title` | §4.7 |
| `Item.Link` | 記事の URL（正規化後） |
| `Item.GUID` | §5.2 |
| `Item.Content` | 抽出した本文（サニタイズは既存パイプライン）。本文なしの場合は空 |
| `Item.PublishedParsed` | §4.6 |

`Feed.Title` は初回クロール時に `feeds.title` に書かれる（既存の
`UpdateFeedAfterFetch` 経由）。一覧ページのタイトルがそのまま
サイドバーの購読名になるので妥当。

### 5.2 GUID 設計

**GUID = 正規化後の記事 URL そのもの**（ハッシュ化しない）。

- F0 のような `hash(内容)` は不要。記事ごとに固有の URL があるという
  前提が F1 の出発点だから、URL がそのまま自然キーになる。
- ハッシュ化しないのは、DB を直接見たときに何の記事か分かる方が
  デバッグしやすいため。`entries.guid` は TEXT で長さ制約もない。
- 記事がリライトされて本文が変わっても GUID は変わらないので、
  `UpsertEntries` の UPDATE 経路で本文だけが差し替わる。既読状態は保たれる。
  これは RSS の挙動と同じで、望ましい。
- **URL が変わると別記事になる**（`/2026/08/foo` → `/blog/foo` のような
  パーマリンク変更）。これはサイト側のリニューアル時に一時的に
  重複エントリーが出るということだが、フィードを配信しているサイトでも
  `guid` を変えれば同じことが起きるので、方式固有の欠陥ではない。

### 5.3 エントリー本文

F0 のような差分表示は不要。本文はそのまま記事本文が入る。
本文が取れなかった場合（§4.5）は**空**にする。UI 側は本文が空でも
タイトルとリンクで読み飛ばせるので、「本文を取得できませんでした」という
プレースホルダ文字列は入れない — 全文検索のインデックスを汚すため。

## 6. 永続化

### 6.1 state の中身（urlpattern）

既存の `scrape_sources.state`（JSON、不透明）をそのまま使う。
**マイグレーションは不要**。

```json
{
  "version": 1,
  "config_hash": "3ab1…",
  "truncated": false,
  "seen": ["https://example.com/news/2026/08/a", "…"],
  "pending": {"https://example.com/news/2026/08/b": 2}
}
```

| フィールド | 用途 |
|---|---|
| `version` | state の JSON フォーマット版。想定外なら state を捨てて再同期（§6.3） |
| `config_hash` | `url_pattern` / `exclude_pattern` / `same_host_only` を正規化した JSON の SHA-256 |
| `truncated` | 候補数上限で切り捨てたか（UI 警告用） |
| `seen` | 取り込み済み記事 URL。文書順ではなく**追加順**で保持し、上限超過時は先頭（古い方）から捨てる |
| `pending` | 取得に失敗中の URL → 連続失敗回数（§4.5） |

`rules_version`（F0 にある、feedla 側の除去規則の版）は **持たない**。
urlpattern の判定はユーザーが書いたパターンだけで決まり、
feedla 側のハードコード規則に依存しないため（URL 正規化規則が変わると
理屈上は影響するが、その場合は `version` を上げれば足りる）。

**サイズ上限**: `seen` は **2000 件**（1 件 100 バイト前後として約 200KB）。
超過分は追加順の古い方から捨てる。捨てた URL が一覧ページに
まだ載っている場合は再度「新着」として取り込まれてしまうが、
GUID が URL ベースなので**重複エントリーにはならず、既読も保たれる**
（`UpsertEntries` の UPDATE 経路）。2000 件を超えて一覧に載り続ける
記事があるサイトは事実上ないので、実害はない。

**`seen` に URL 全体を持つのはサイズが無駄ではないか**という点について:
SHA-256 の 16 進 64 文字より、典型的な記事 URL（60〜80 文字）の方が
むしろ長いこともあるが、**プレビュー UI で「この URL は取り込み済み」と
表示できる**という運用上の利点を取ってそのまま持つ。切り詰めるなら
`seen` を「URL の SHA-256 先頭 16 バイト」に変える余地は残っている
（`version` を上げれば移行できる）。

### 6.2 なぜ entries テーブルを既知 URL の集合として使わないのか

「`entries` に `url` があるのだから、`SELECT 1 FROM entries WHERE feed_id=? AND url=?`
で既知判定すればよく、state は要らないのでは」という案を検討したが**採らない**。
F0 設計 §6.4 と同じ理由:

- `entries` は GC で 30 日で消える（DESIGN.md「GC / リテンション」）。
  消えた瞬間、一覧にまだ載っている古い記事が**全部「新着」として再取得される**。
  記事本文の再 fetch が N 件走り、サイトに無用な負荷をかける。
- **保持期間の異なるデータに依存してはいけない**。エントリーは読んだら
  消えてよいデータ、state は購読が続く限り消してはいけないデータ。
- `pending`（失敗リトライ回数）は entries には置き場所がない。

### 6.3 state の無効化と再同期

無効化のトリガは 2 つ:

1. ユーザーが `url_pattern` / `exclude_pattern` / `same_host_only` を変更した
   （`config_hash` 不一致）
2. state の JSON が壊れている / `version` が想定外

F0 と違い、**どちらの場合も `seen` を捨てない**。これが F0 との重要な差。

- F0 の state は「前回のページ内容」なので、比較の前提が変われば
  内容そのものが無意味になる（だから捨てて再同期する）。
- F1 の `seen` は「**取り込んだ記事 URL の履歴**」であり、
  パターンを変えても履歴の意味は変わらない。捨てると、
  パターンを 1 文字直しただけで**取り込み済みの記事が全部もう一度
  未読として湧く**（かつ本文の再 fetch が N 件走る）ことになり、
  F0 設計 §6.6 が避けようとしたのと同じ失敗になる。
- `config_hash` の不一致は「パターンが変わった」という**記録目的だけ**に使い、
  挙動は変えない（次回のクロールから新しいパターンが効く、それだけ）。
- 2（壊れた state）の場合は `seen` を空として扱う。この場合に限り
  一覧の記事が再取り込みされるが、GUID が URL ベースなので
  **重複エントリーにはならず、既読も保たれる**（本文の再 fetch は走る）。

この「捨てても壊滅しない」性質は §5.2 の URL ベース GUID がもたらしたもので、
F1 の state 設計を F0 よりずっと緩くしている。

### 6.4 OPML・バックアップとの関係

F0 と同じ扱いを踏襲する。

- **OPML export**: `kind != "feed"` の購読は除外する。現状の実装は
  `s.Kind == "pagewatch"` の等値比較なので、**`!= "feed"` に一般化する**
  （§7.1 で kind が増えるため）。
- **OPML import**: `feed_url` が既知のスクレイプ接頭辞で始まる outline は無視する。
  現状の `strings.HasPrefix(f.FeedURL, crawler.ScrapePrefix)` を
  接頭辞リストに対する判定に一般化する。
- `GET /api/v1/scrape_sources` が全設定を JSON で返すので、
  urlpattern の設定もそのままバックアップ対象に入る（実装変更不要）。
- **state はバックアップ JSON に出さない**（既存の `scrapeSourceView` が
  State を含まない設計をそのまま利用）。失われても §6.3 のとおり
  重複エントリーにはならない。

## 7. crawler への統合

### 7.1 疑似スキームを 2 つに増やす

`feeds.feed_url` の接頭辞を `urlpattern:` に増やす。
`pagewatch:` と同じく、実 URL の前に方式名を置く。

```go
// crawler
const (
    PagewatchPrefix  = "pagewatch:"
    URLPatternPrefix = "urlpattern:"
)
```

**共通接頭辞（`scrape:` 等）に統一しない理由**: `SubscriptionView.Kind` を
`feeds.feed_url` の接頭辞から SQL だけで導出している（`store/subscriptions.go`
の `subscriptionViewColumns`）ため。共通接頭辞にすると kind を出すのに
`scrape_sources` との JOIN が必要になり、F0 設計 §6.2 が
「`ClaimDueFeeds` / `Feed` 型を無変更に保つ」ために選んだ性質を失う。
方式ごとに接頭辞を持てば、SQL は CASE の分岐が 1 本増えるだけで済む。

既存の `ScrapePrefix` は `PagewatchPrefix` に改名し、
接頭辞 → kind の対応表を 1 箇所に置く:

```go
// crawler
var scrapePrefixes = map[string]extract.Kind{
    PagewatchPrefix:  extract.KindPageWatch,
    URLPatternPrefix: extract.KindURLPattern,
}

// cutScrapePrefix returns (実URL, kind, true) for a scrape-backed feed_url.
func cutScrapePrefix(feedURL string) (string, extract.Kind, bool)
```

`store` 側は crawler を import できない（依存方向の制約）ため、
現状どおり接頭辞の文字列を SQL 用に複製する。**複製が 2 箇所に増えるので、
接頭辞の一致を検証するテストを store 側に置く**（`crawler` を
import できるのはテストではなく…という制約を避けるため、
`internal/store` のテストではなく `internal/crawler` のテストから
`store.ScrapePrefixes()` のような公開値を突き合わせる）。

`crawlOne` の分岐は、既存の `isScrape` bool を kind に置き換える:

```go
target, kind, isScrape := cutScrapePrefix(f.FeedURL)
...
if isScrape {
    parsed, scrapeState, err = c.extractPage(ctx, f.ID, kind, target, fr, now)
} else {
    ...
}
```

`extractPage` は現在 `src.Kind != KindPageWatch` を弾いているので、
**kind → Extractor のレジストリ**に変える（`Crawler.pagewatch` フィールドの
コメントが既に予告している変更）。

### 7.2 個別ページ取得の置き場所

`extractPage` が `*ParsedFeed` を返した直後、`UpsertEntries` の前に
「本文が空のエントリーを個別ページから埋める」処理を挟む。

既存の `applyFulltext`（実 feed 向け）と**同じ形**なので、共通部分を切り出す:

```go
// 現状: applyFulltext(ctx, feedID, parsed) が
//   ・feed_fulltext の有無を見る
//   ・ExistingEntryGUIDs で新規だけに絞る
//   ・maxFulltextFetchesPerCrawl で上限を掛ける
//   ・extractEntryFulltext で本文を差し替える
// このうち下 3 つを fillEntryBodies(ctx, entries, limit) として切り出し、
// urlpattern 経路からも呼ぶ。
```

ただし urlpattern 経路では既存判定に `ExistingEntryGUIDs` を使わない
（state の `seen` が既に新着だけに絞っている）。また、
**失敗した URL を crawler が extract に伝え返す必要がある**（§4.5 の
`pending` 更新）。これを `extract.Result` の往復で表現すると
インターフェースに HTTP の匂いが漏れるので、次のようにする:

- `urlpattern.Extract` は state を**「今回の候補をすべて seen に入れた形」では返さない**。
  代わりに `Result.State` には「前回の seen ＋ `pending` の更新なし」の状態を入れ、
  **新着候補は `Feed.Items` としてのみ返す**。
- crawler は取り込み結果（成功 URL の集合・失敗 URL の集合）を確定させたあと、
  `urlpattern.CommitState(prevState, succeeded, failed) json.RawMessage`
  という**パッケージ公開のヘルパ**を呼んで最終 state を作る。
  この関数は純粋関数（HTTP を知らない）なので、依存方向の原則は保たれる。

`extract.Extractor` インターフェース自体は変えず、方式固有のヘルパを
方式のパッケージに置くだけなので、pagewatch 側には影響しない。

### 7.3 取得件数の上限と適応間隔

| 項目 | 値 | 根拠 |
|---|---|---|
| 1 クロールあたりの個別ページ取得 | `max_items_per_crawl`（既定 20、上限 50） | 既存の `maxFulltextFetchesPerCrawl` と同じ値。1 サイトの 1 ターンが数十リクエストに膨らむのを防ぐ |
| 初期 `fetch_interval_sec` | 3600（1 時間） | pagewatch と同じ。通常フィードの 1800 より緩い |
| 最小間隔 | 既存の `MIN = 10min` | 適応制御の下限は共通 |

適応間隔（`nextIntervalOnSuccess`）は `newCount > 0` で駆動される既存ロジックを
そのまま使う。§4.5 のリトライ経路が UPDATE になり `newCount` に数えられないので、
**失敗リトライ中のサイトの間隔が誤って縮まることはない**。

### 7.4 robots.txt — F1 では実装する

F0 設計 §10.3 は「robots.txt を F0 で実装しない」理由として
「ユーザーが明示的に 1 件ずつ登録した URL であり、**クローラによる URL の
自動発見・追跡がない**」ことを挙げ、そのうえで
「**Phase F1（一覧ページ + N 件の個別ページ取得）では取得件数が桁で増えるため、
そこで robots.txt 対応を必須要件として再検討する**」と申し送っている。

F1 はまさに「URL を自動発見して追跡する」機能なので、**実装する**。
F0 が挙げた不要理由が F1 では 1 つも成り立たない以上、
ここで実装しないなら申し送りを反故にする理由を別途示さねばならず、
それは無い。

**実装範囲（最小）**:

- 個別記事ページの取得**のみ**を robots.txt で判定する。一覧ページ自体は
  「ユーザーが明示的に登録した 1 URL」なので F0 と同じ扱いとし、判定しない。
- `User-agent: *` と feedla 自身の UA を見て `Disallow` / `Allow` の
  最長一致で判定する。`Crawl-delay` は**見ない**（既存の `HostSemaphore` が
  同一ホスト最低 1 秒間隔を保証しており、それ以上の待機は
  クロールプールの占有時間を伸ばすだけなので）。
- ホストごとに **24 時間キャッシュ**（プロセス内メモリ。DB には持たない）。
  取得失敗・404・5xx は「制限なし」として扱う（robots.txt が無いサイトは
  多数派で、取得できないことを禁止と解釈すると機能しない）。
- 実装は `internal/crawler/robots.go` に自前で置く。
  外部依存（`temoto/robotstxt` 等）を足さない — 必要なのは
  `Disallow`/`Allow` の前方一致と `*`/`$` のワイルドカードだけで、
  100 行程度で書ける。
- Disallow された記事はエントリーを**タイトル＋リンクのみで作る**
  （本文なし）。**エントリー自体を作らないのは行き過ぎ**で、
  「その URL が存在する」ことは一覧ページ（ユーザーが読む権利のあるページ）に
  書いてある事実だから。

### 7.5 エラー分類

F0 設計 §7.4 の表に、F1 固有の行を足す。

| 事象 | 分類 | `error_count` | 備考 |
|---|---|---|---|
| 一覧ページの HTTP エラー / タイムアウト | external | ++ | 既存と同じ |
| 一覧ページの charset デコード / パース失敗 | external | ++ | 既存と同じ |
| **`url_pattern` にマッチする URL が 0 件** | external | ++ | パターン破損・サイト構造変更・bot ブロックの疑い。§9.3 で UI に出す |
| **個別記事の取得失敗** | （エラーにしない） | 据置 | §4.5 のリトライ経路。一覧が取れている以上、購読自体は健全 |
| store 書き込み失敗 | internal | 据置 | 既存の方針どおり |

「マッチ 0 件」を external エラーにするのは、pagewatch の「ブロック 0 件」と
同じ狙い（サイトの全面リニューアルや bot 対策を `error_count` 経由で
UI に浮かび上がらせる）。ただし**個別記事の失敗はエラーにしない** —
記事 1 本が 404 なだけで購読が自動解除の閾値に近づくのは明らかに行き過ぎ。

## 8. API

### 8.1 既存 CRUD の一般化

既存の `/api/v1/scrape_sources` 系エンドポイントは、
**kind を受け付ける形に一般化するだけ**で足りる。

```
POST   /api/v1/scrape_sources          {kind: "urlpattern", url, folder_id?, title?, config}
GET    /api/v1/scrape_sources
GET    /api/v1/scrape_sources/{id}
PATCH  /api/v1/scrape_sources/{id}     {config}
POST   /api/v1/scrape_sources/{id}/preview
```

- `handleCreateScrapeSource` は現在 `kind != pagewatch` を 400 で弾いている。
  ここを許可リストに変え、kind ごとに config のバリデータ
  （`pagewatch.ParseConfig` / `urlpattern.ParseConfig`）を切り替える。
- `feedURL := crawler.ScrapePrefix + req.URL` を kind に応じた接頭辞に変える。
- `handlePatchScrapeSource` も同様に、**保存済みの kind** に対応する
  バリデータを使う（リクエストの kind は見ない。kind の変更は許さない）。
- クオータ（`MaxScrapeSources`）・レート制限（`feedAddLimiter`・
  `previewLimiter`）・所有者チェック（作成者 or admin）は**すべて既存のまま流用**する。

### 8.2 購読前プレビュー（新規エンドポイント）

**正規表現を手で書く方式は、プレビューが無いと実用にならない。**
既存の preview は `{id}` を要求するため購読後にしか使えないので、
購読前に使えるものを足す:

```
POST /api/v1/scrape_sources/preview   {kind, url, config}  → 抽出結果
```

- レスポンスは kind ごとに形が違う。pagewatch は既存の `{blocks: [...]}`、
  urlpattern は:

  ```json
  {
    "candidates": [
      {"url": "https://example.com/news/2026/08/a", "title": "…", "seen": false}
    ],
    "total_links": 143,
    "matched": 18,
    "truncated": false
  }
  ```

  `total_links`（フィルタ前の同一ホストリンク総数）を返すのは、
  「0 件です」と言われたときにパターンが厳しすぎるのか、
  そもそもリンクが取れていない（bot ブロック等）のかを
  ユーザーが切り分けられるようにするため。
- `{id}/preview` の方も urlpattern の場合はこの形を返す
  （保存済みの config で実行し、`seen` を state と突き合わせて埋める）。
  購読前プレビューでは `seen` は常に false。

**セキュリティ**: このエンドポイントは**認証済みユーザーが任意の URL を
feedla に取得させられる**。既存の `{id}/preview` と
`POST /scrape_sources`（どちらも同じ性質を持つ）と同格に扱う:

- SSRF 対策 dialer（プライベート IP 拒否）・レスポンス上限・
  リダイレクト制限は `Fetcher` 側にあるのでそのまま効く。
- `previewLimiter` によるレート制限を必ず通す。
- **所有権チェックが存在しない**（対象リソースがまだ無いので当然）点が
  `{id}/preview` との違い。したがって**認証必須**であることと
  レート制限が唯一の防波堤になる。CLAUDE.md の要求どおり
  IDOR テストを書く（未認証で 401、他人の scrape source の id を
  混ぜられないこと、レート制限が user 単位で効くこと）。

### 8.3 既存 API への影響

- `SubscriptionView.Kind` に `"urlpattern"` が増える。SQL の CASE を
  2 分岐にする（§7.1）。**API の型は変わらない**（既に文字列）。
- `POST /api/v1/subscriptions`（通常の購読）は**変更しない**。
  フィード未検出時は従来どおり 502 を返し、UI が選択肢を出す（§9.1）。

## 9. Web UI

### 9.1 登録導線

`AddSubscriptionDialog` の 502（フィード未検出）時の分岐に選択肢を 1 つ足す。
現在は「ページの更新を監視する」だけなので、次の 2 択にする:

```
このページにフィードが見つかりませんでした。

[記事一覧として取り込む]   一覧ページから記事リンクを拾い、記事ごとに読めるようにします
[ページの更新を監視する]   ページ全体の変化を 1 件の記事として通知します（既存）
```

「記事一覧として取り込む」を選ぶと、同じダイアログ内で次のステップに進む:

1. **URL パターン入力**（テキスト 1 行）。空のままでも次に進める。
2. **[プレビュー]** ボタン → `POST /scrape_sources/preview` を叩き、
   マッチした URL とタイトルの一覧、`総リンク数 / マッチ数` を表示。
3. パターンを直して 2 を繰り返す。
4. **[この設定で購読]** → `POST /scrape_sources`。

パターンが空のときのプレビューは「同一ホストのリンクを**全部**返す」
（`matched == total_links`）。ここから拾いたい URL を眺めて
パターンを書くのが実際の使い方になるので、空パターンを
エラーにせず「まず全部見せる」挙動にしておくことに意味がある。
ただし**購読時は空パターンを 400 で弾く**（一覧ページ自身や
`/about` まで記事として取り込まれてしまうため）。

登録時には pagewatch と同じ注意喚起を出す:
「サイト運営者はフィード配信を意図していません。一覧の取得は 1 時間に 1 回、
記事本文の取得は 1 回あたり最大 20 件です」。

### 9.2 一覧での区別

`SubscriptionTree` で `kind === 'urlpattern'` の購読にアイコンを付ける
（pagewatch の 👁 と別のもの。📄 など）。未読数・グループ・rating・pin・
全文検索は entries を共有しているのでそのまま動く。

### 9.3 抽出設定パネル

`FeedDetailOverlay` に、pagewatch の `PagewatchSettings` と並ぶ
`UrlPatternSettings` を追加する（kind で出し分け）:

- 一覧ページ URL（読み取り専用）
- `url_pattern` / `exclude_pattern` の編集（保存すると PATCH）
- `same_host_only` / `fulltext` / `title_source` のトグル
- `max_items_per_crawl` の数値入力
- **[いま取得して確認]** → `{id}/preview`。マッチした URL・タイトル・
  取り込み済みかどうか（`seen`）を一覧表示
- **パターンが機能していない警告**: `error_count > 0` かつ
  `last_error` が「マッチ 0 件」を含む場合に、
  「URL パターンにマッチする記事が見つかりませんでした。
  サイトの構成が変わった可能性があります」と赤字で出し、
  プレビューへ誘導する。方針検討ドキュメントが
  「壊れたことをどう検知してユーザーに知らせるかの設計が追加で要る」と
  指摘した点への回答がこれ。

### 9.4 エントリー側の導線

pagewatch の §9.4（差分ブロックを「無視する」ボタンで正規表現に変換）に
相当する誤検知回復導線は、F1 では**エントリー単位の「この URL を除外する」**になる。
記事ペインのエントリーメニューに項目を足し、押すとその記事 URL を
`exclude_pattern` に追加（`regexp.QuoteMeta` でエスケープした完全一致を
`|` で連結）し、あわせてそのエントリーを既読にする。

正規表現を直接書かせずに「これは記事じゃない」と教えられるようにする、という
F0 §9.4 と同じ思想。ただし F1 では優先度を下げてよい（誤爆しても
「関係ないページが 1 件混じる」だけで、F0 の誤検知のように
毎回通知が飛ぶわけではないため）。作業分解では任意項目に置く。

## 10. 設定・上限・礼儀

### 10.1 Config の全体像とデフォルト値

```go
// internal/extract/urlpattern
type Config struct {
    URLPattern       string `json:"url_pattern"`                  // 必須
    ExcludePattern   string `json:"exclude_pattern,omitempty"`
    SameHostOnly     *bool  `json:"same_host_only,omitempty"`     // 既定 true
    Fulltext         *bool  `json:"fulltext,omitempty"`           // 既定 true
    TitleSource      string `json:"title_source,omitempty"`       // "list"(既定) | "article"
    MaxItemsPerCrawl int    `json:"max_items_per_crawl,omitempty"`// 既定 20、上限 50
    MinTitleChars    int    `json:"min_title_chars,omitempty"`    // 既定 2
    LinkScopeSelector string `json:"link_scope_selector,omitempty"` // 予約、F1 では未使用
}
```

| 項目 | 値 | 備考 |
|---|---|---|
| `url_pattern` 長 | 最大 1000 文字 | F0 の `ignore_patterns` と同じ |
| 収集する `<a>` | 5000 件 | 超過は切り捨て |
| フィルタ通過候補 | 500 件 | 超過は切り捨て + `truncated` |
| 1 クロールの個別取得 | 20（上限 50） | §7.3 |
| `seen` 件数 | 2000 | §6.1 |
| 記事 1 本の失敗リトライ | 3 回 | §4.5 |
| robots.txt キャッシュ | 24 時間 | §7.4 |
| レスポンス上限 | 既存の 10 MiB | Fetcher 側 |

正規表現は API 受信時に `regexp.Compile` を通して 400 で弾く（F0 と同じ）。
RE2 なのでバックトラック爆発はしないが、
**5000 リンク × 巨大パターンの CPU 時間**を抑えるために長さ上限を設ける。

### 10.2 既存の礼儀機構で足りるか

`HostSemaphore`（同一ホスト同時 2・最低 1 秒間隔）が個別記事の取得にも
そのまま効くので、20 件の取得は最短でも約 10 秒（2 並列 × 1 秒間隔）かけて
行われる。これは「1 サイトに 20 リクエストを一瞬で叩き込む」ことにはならない、
という点で既存機構だけで十分な礼儀を満たす。

**F1 で追加する礼儀は robots.txt（§7.4）だけ**。

### 10.3 著作権・利用規約

F0 §13 と同じ整理:

- 取得したページを手元の DB に保存するのは私的利用の範囲。
  登録時の注意喚起（§9.1）に留め、それ以上の制限は設けない。
- ただし F1 は**記事本文を丸ごと保存する**点で F0 より踏み込んでいる。
  `Config.fulltext = false`（タイトルとリンクのみ）という逃げ道を
  用意してあることを、設定 UI の説明文で明示する。
- **実在サイトの HTML をリポジトリに置かない**線は F1 でも維持する。
  テストフィクスチャは `tools/htmlskeleton`（F0 で作成済み）で
  匿名化したものを使い、匿名化検査テストの対象に含める。

## 11. テスト計画

| 層 | 内容 |
|---|---|
| `extract/urlpattern` 単体 | 一覧 HTML → 期待候補列。DB も HTTP も使わない。<br>・相対 URL が一覧ページ基準で絶対化されること<br>・`utm_*` 付きリンクと素のリンクが同一候補に畳まれること<br>・フラグメント違いが 1 件に畳まれること<br>・外部ホストが `same_host_only` で落ちること／false で残ること<br>・サムネイルリンクとタイトルリンクが重複排除され、テキストのある方が残ること<br>・`exclude_pattern` が効くこと<br>・候補 500 件超で `truncated` が立つこと |
| state 単体 | `CommitState` が成功 URL のみ `seen` に入れ、失敗 URL の `pending` を +1 すること。3 回で諦めて `seen` に入ること。`seen` 2000 件超で古い方から捨てられること。壊れた JSON でパニックせず空 `seen` に落ちること |
| 日付抽出単体 | `internal/fulltext.ExtractPublished`: OGP / JSON-LD / `<time>` の優先順位。未来日を弾くこと。どれも無ければ ok=false |
| robots.txt 単体 | `Disallow`/`Allow` の最長一致、`*`/`$`、UA の選択、404/5xx は許可扱い、キャッシュが 24 時間効くこと |
| crawler 統合 | `httptest.Server` に一覧＋記事 3 本を置き、<br>・初回で 3 件、2 回目で 0 件<br>・一覧に 1 本増えたら 1 件（既存 3 本は再 fetch されないこと＝リクエスト数で検証）<br>・記事 1 本が 500 を返す → その記事だけ未取り込み、次回リトライされること<br>・3 回失敗後はタイトルのみで取り込まれ、以後 fetch されないこと<br>・`max_items_per_crawl` を 2 にすると 1 回のクロールで 2 本だけ取ること<br>・記事が `Disallow` 配下なら本文なしで取り込まれること |
| store | `scrape_sources` の kind が `urlpattern` でも CRUD/CASCADE が動くこと（既存テストの kind 差し替え） |
| API | `POST /scrape_sources {kind:"urlpattern"}` → `GET /subscriptions` に `kind:"urlpattern"` で出る。不正な正規表現・空 `url_pattern` が 400。`POST /scrape_sources/preview` が候補を返す |
| **IDOR**（CLAUDE.md 必須） | ・他ユーザーの scrape source を PATCH できないこと（既存テストに kind を足す）<br>・`{id}/preview` を非所有者が叩けないこと<br>・`POST /scrape_sources/preview`（新規・所有権チェック不能）が未認証で 401、レート制限が user 単位で効くこと |
| OPML | `urlpattern:` 接頭辞の購読が export されず、import でも無視されること |
| e2e | 一覧ページのフィクスチャを 1 件購読 → 記事が複数出る → **末尾で必ず購読解除**（e2e スイートは DB とサイドバーを共有しており、残すと後続 spec の順序を壊す） |

## 12. 作業分解

| # | 作業 | 依存 | 完了条件 |
|---|---|---|---|
| 1 | `internal/extract/urlpattern`: リンク収集・正規化・フィルタ・候補生成 + `Config`/`ParseConfig` | — | 単体テスト緑（DB・HTTP 不要） |
| 2 | 同: `State` と `CommitState`（seen/pending の更新規則） | 1 | 単体テスト緑 |
| 3 | `internal/fulltext.ExtractPublished`（OGP / JSON-LD / `<time>`） | — | 単体テスト緑 |
| 4 | `internal/crawler/robots.go` + 24h キャッシュ | — | 単体テスト緑 |
| 5 | crawler: 接頭辞レジストリ化（`ScrapePrefix` → `PagewatchPrefix` + `URLPatternPrefix`）、`extractPage` の kind ディスパッチ、store 側 SQL の CASE 追加 | — | 既存テスト緑のまま、pagewatch の挙動が変わらないこと |
| 6 | crawler: 個別ページ取得ループ（`applyFulltext` からの共通部切り出し、上限・リトライ・robots 判定・state コミット） | 1〜5 | crawler 統合テスト緑 |
| 7 | API: kind 一般化（create/patch のバリデータ切り替え）+ `POST /scrape_sources/preview` | 5,6 | API テスト + IDOR テスト緑 |
| 8 | Web UI: 登録導線の 2 択化、パターン入力＋プレビュー、`UrlPatternSettings`、アイコン | 7 | 手動確認 + e2e |
| 9 | OPML export/import の `kind != "feed"` 一般化 | 5 | round-trip テスト緑 |
| 10 | （任意）エントリーの「この URL を除外する」導線（§9.4） | 8 | — |
| 11 | ドキュメント更新（DESIGN.md、方針検討ドキュメントの Phase F1 のステータス、README） | 8 | — |

**Phase F1 の受け入れ条件**: フィードを配信していない一覧ページを
2 種類（記事 URL に日付を含む形式／連番 ID の形式）購読し、
新着が記事単位のエントリーとして読めること。判定は
匿名化フィクスチャによるゴールデンテストを主、実運用観察を従とする
（F0 §14.3 と同じ方針）。

## 13. 既知の制約・持ち越す論点

- **正規表現が書けないと使えない**。これが F1 の最大の制約で、
  Phase F2（GUI クリック選択）または F3（自動検出）で解消する。
  F1 のプレビュー（§8.2）は「書けるが自信がない」層までしか救わない。
- **一覧の 1 ページ目しか見ない**（§4.4）。購読開始より前の記事は
  `max_items_per_crawl` の持ち越し分を除いて取り込まれない。
- **記事の更新を検知しない**。一度 `seen` に入った URL は再取得しない。
  記事が後から加筆されても本文は初回取得時のまま。これは 1+N のコストを
  抑えるための意図的な割り切りで、必要になれば
  「`seen` に記事の `Last-Modified` を持たせて条件付き GET で再取得」を
  後から足せる（`State.version` を上げる）。
- **SPA の一覧ページは取れない**（方式 E、恒久的に対象外）。
  素の GET でリンクが 0 件なら §7.5 の「マッチ 0 件」エラーになり、
  UI にはパターン破損と同じ見え方で出る。両者を区別する材料は
  プレビューの `total_links`（0 なら SPA/bot ブロック、
  0 でなければパターンの問題）で与える。
- **bot 対策サイトは対応できない**。F0 と同じく、
  回避策は講じず「対応できないサイトがある」ことを前提として明示する。
- **方式 B0（sitemap.xml / JSON-LD からの一覧取得）は F1 に含めない**。
  §4.6 で「記事ページから公開日時を取る」部分だけ先取りする。
  一覧そのものを sitemap から取る案は、`lastmod` の信頼性がサイト依存で
  効果が読めないため、F1 の実運用でパターン指定が辛いサイトの傾向が
  見えてから判断する。
- **`internal/extract` 配下でのコード共有**: URL 正規化
  （トラッキングパラメータ除去・絶対化）は pagewatch の `attrs.go` に
  既にある。方針検討ドキュメントが認めているとおり
  `internal/extract` 内でのパッケージ間再利用は許容されるので、
  `internal/extract/urlnorm`（または `internal/extract` 直下）に
  切り出して両方から使う。実装時に pagewatch 側の golden テストが
  変わらないことを確認する。
