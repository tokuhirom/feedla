# 方式 B1: 一覧ページからの記事抽出（selector）詳細設計

ステータス: **設計案（未実装）**。
[フィード非提供サイトの購読機能 — 方針検討](feedless-site-subscription.md) の
**方式 B1（CSS セレクタによる一覧抽出）** の実装設計。全体方針・他方式との
比較はそちらを、先行して実装済みの単一ページ監視は
[方式 A: 単一ページ監視（pagewatch）詳細設計](feedless-site-subscription-pagewatch.md)
（以下「F0 設計」）を参照する。

## 0. フェーズ分割 — セレクタが先、GUI が後

本設計は 2 つのフェーズに分かれる。

| | Phase F1（本設計の主対象） | Phase F2 |
|---|---|---|
| 何を作るか | CSS セレクタで一覧から記事を抽出するパイプライン | セレクタを**クリックで作る** GUI |
| セレクタの入手方法 | ユーザーがテキストで入力し、プレビューで確認しながら直す | プレビュー上で記事要素をクリック→自動生成 |
| 到達点 | 「セレクタが書ける人なら任意のサイトを購読できる」 | 「セレクタを知らなくても購読できる」 |

**最終的に目指すのは F2（Feedly の Feed Builder 相当）**である。F1 を先に作るのは、
F2 が「セレクタを生成する UI」であって「別の抽出方式」ではないから — F2 は
F1 が保存するのと**同じ形のセレクタを、別の入力手段で作るだけ**であり、
抽出・取得・エントリー化のパイプラインは丸ごと共有される。

したがって F1 の設計判断は、**F2 を後から無理なく載せられること**を制約として持つ。
本設計では §10 で F2 の見通しを示し、F1 の各所（Config の形、プレビュー API の
返す情報）がそれを縛らないことを確認する。

## 1. スコープ

**やること（F1）**: ユーザーが登録した 1 つの**一覧ページ**（ブログのトップ、
お知らせ一覧、ニュースの特集面）を定期取得し、ユーザーが指定した
**CSS セレクタ**で「記事 1 件に相当する繰り返し要素」を取り出し、
その中からリンク・タイトル・日付を拾い、初出の記事について
**個別ページを取得して本文を抽出**し、**記事 1 件 = エントリー 1 件**として
既存の未読管理に流す。

F0 との違いは「1 URL = 1 エントリー着地点」という制約が外れることで、
方針検討ドキュメントの対象サイト例パターン 1〜3（企業のお知らせ一覧、
フィード配信忘れの個人ブログ、ニュースの特集面）＝**本来の要望の中心**が
ここでカバーされる。

**やらないこと（F1 の非スコープ）**:

- GUI クリック選択（Phase F2。§10 に設計見通しのみ）
- 記事一覧の自動検出・セレクタの自動推測（方式 B2 / Phase F3。ただし
  §10.3 のセレクタ生成アルゴリズムは F3 の部分的な先取りになる）
- sitemap.xml / JSON-LD からの一覧取得（方式 B0。§4.6 で日付抽出にのみ使う）
- JS レンダリング（方式 E、恒久的に対象外）
- ページネーションを辿って過去記事を遡ること（§4.4）

**F0 を置き換えるものではない**。1 ページの中身が書き換わるだけのサイト
（ステータスページ、最新 N 件を 1 ページに載せる日記）は引き続き pagewatch が
担当し、selector は「記事ごとに個別 URL を持つサイト」を担当する。
`scrape_sources.kind` で並存する。

## 2. 全体像

```
 Scheduler(既存 30s tick)
   └─ ClaimDueFeeds ──▶ crawlOne(feed)
                          │
                          ├─ feed_url の接頭辞は？
                          │     なし        ─▶ 既存の ParseFeed 経路（変更なし）
                          │     pagewatch:  ─▶ 既存の extractPage 経路（変更なし）
                          │     selector:
                          ▼
                  Fetcher.Fetch(一覧URL, Accept: text/html)  ← 条件付きGETは既存のまま
                          │ 304 → 何もせず interval 更新（最安）
                          ▼ 200
                  charset デコード（既存 DecodeUTF8）
                          │
                          ▼
        internal/extract/selector.Extract(html, prevState)
                  ├─ x/net/html でパース
                  ├─ item_selector で記事要素の列を取得（cascadia）
                  ├─ 各要素から link / title / date / summary を取得
                  ├─ URL 正規化（絶対化・トラッキングパラメータ除去・フラグメント除去）
                  ├─ 同一 URL を重複排除
                  └─ prevState.seen に無い URL = 新着候補
                          │
                          ▼
                  *gofeed.Feed（新着候補 N 件。本文は空、Link と Title のみ）
                          │
                          ▼  ← ここから crawler の責務（extract は HTTP を知らない）
              新着候補を最大 max_items_per_crawl 件まで
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

**設計の要（本設計で新たに要るもの）**: 「一覧の解析（純粋関数）」と
「個別ページの取得（HTTP）」を分離し、後者を crawler 側に置く。

## 3. パッケージ構成と依存方向

```
internal/extract/            共通型（Extractor, Input, Result）。DB も HTTP も知らない
  pagewatch/                 方式A（実装済み）
  selector/                  方式B1（新規）。x/net/html + cascadia だけに依存
internal/fulltext/           Readability 本文抽出（実装済み）。selector から import しない
internal/crawler/            → internal/extract と internal/fulltext を import
internal/store/              scrape_sources の CRUD。どちらも import しない
```

### 3.1 CSS セレクタライブラリ — 新規の外部依存は増えない

F0 設計 §13 は `Config.scope_selector`（CSS セレクタで監視範囲を指定する枠）を
**「実装には `cascadia` 等の依存追加が必要になるため F0 では見送る」**と書いている。
**この理由は既に成立していない。**

```
$ go mod why github.com/andybalholm/cascadia
github.com/tokuhirom/feedla/internal/fulltext
codeberg.org/readeck/go-readability/v2
github.com/go-shiori/dom
github.com/andybalholm/cascadia
```

`cascadia` は全文抽出機能（`internal/fulltext` → go-readability）の推移依存として
**既にビルドに入っている**。`go.mod` の `// indirect` を外して直接依存に
昇格させるだけで、**モジュール数もバイナリサイズも 1 バイトも増えない**。
DESIGN.md の「シングルバイナリ・外部依存を増やさない」という中核目標と
衝突しないことを、この節をもって確認しておく。

`cascadia` は `x/net/html` の DOM をそのまま扱うライブラリなので、
pagewatch と同じパース結果に対して使える（`goquery` のような
別の DOM 表現への変換は不要）。

### 3.2 依存方向

- `internal/extract/selector` は `internal/fulltext` を **import しない**。
  本文抽出は HTTP 取得とセットでしか意味がなく、その HTTP は crawler の
  責務だから。selector は「一覧 HTML → 記事のリンクとメタデータの列」だけを
  返す純粋関数に徹する。この線を引くことで、selector の単体テストは
  ネットワークも DB も使わずに書ける（F0 と同じ性質を保つ）。

### 3.3 既存の Extractor インターフェースで足りるか

`extract.Extractor` は `Extract(ctx, Input) (*Result, error)` で、
**HTML 1 枚を受け取って 1 つの疑似フィードを返す**。本方式は 1+N 取得なので
一見合わないが、次のように解釈すれば**インターフェースは変更不要**である。

- `Result.Feed.Items` は「タイトルとリンクだけが埋まった、本文が空の記事」の列。
  これは RSS の世界では珍しくない形（本文を配信しないフィード）で、
  `gofeed.Feed` 中間表現として自然に表現できる。
- 本文の充填は crawler 側の後処理。既に `applyFulltext`（実 feed の
  entry を全文化する処理）が「`ParsedFeed.Entries` を in-place で書き換える」
  という同じ形をしており、**同じ関数を共用できる**（§7.2）。

F0 設計 §13 の申し送り「`Extractor` インターフェースと不透明 `State` が
一覧抽出方式でも無理なく使えるかを検証する」に対する回答が、この節である。
**結論: 使える。インターフェースの変更は不要**。

## 4. 抽出パイプライン（selector）

### 4.1 なぜ CSS セレクタなのか

方針検討ドキュメントは B1（GUI クリック選択）、B1.5（URL パターン）、
B2（自動検出）を挙げているが、**B1 を本命とする**。

| | CSS セレクタ（採用） | URL パターン（B1.5） | 自動検出（B2） |
|---|---|---|---|
| 取れる情報 | **タイトル・リンク・日付・抜粋を個別に指定できる** | リンクとそのテキストのみ | リンクとテキスト（推測） |
| 一覧内の位置指定 | できる（サイドバーの「人気記事」を除外できる） | できない（URL が同じ形なら混ざる） | 一覧候補として別扱いにはなる |
| ユーザーの入力 | セレクタ 1〜4 本 | 正規表現 1 本 | なし |
| **GUI との相性** | **クリック→セレクタ生成が自然**（F2 に直結） | クリックから正規表現は作りにくい | GUI 不要だが精度が出ない |
| 壊れ方 | マークアップ変更で 0 件になる（検知しやすい） | URL 設計変更で 0 件（頻度は低い） | 静かに誤検出する（**最悪**） |

決め手は**最終形が GUI（F2）であること**。クリックした要素から CSS セレクタを
作るのは素直だが、クリックから URL 正規表現を作るのは無理がある
（クリックした 1 件の URL から「記事 URL 一般の規則」を推測せねばならない）。
B1.5 を経由すると、F2 に進む時点で設定の形が変わり、
既存購読の移行が必要になる。**最初から F2 と同じ形の設定を保存する**方が良い。

CSS セレクタの弱点（マークアップ変更で壊れる）は、
壊れたことが「マッチ 0 件」として明確に観測できるので、
UI で警告して直させる導線（§9.3）で対処する。
方針検討ドキュメントが「壊れたことをどう検知してユーザーに知らせるかの
設計が追加で要る」と指摘した点への回答がこれ。

### 4.2 セレクタの構成

Feedly の Feed Builder と同じく、**記事コンテナを起点に、その内側を相対セレクタで指す**。

```go
// internal/extract/selector
type Config struct {
    ItemSelector    string `json:"item_selector"`              // 必須。記事1件に相当する繰り返し要素
    LinkSelector    string `json:"link_selector,omitempty"`    // item 内。既定は後述の推論
    TitleSelector   string `json:"title_selector,omitempty"`   // item 内。既定はリンクのテキスト
    DateSelector    string `json:"date_selector,omitempty"`    // item 内。省略可
    SummarySelector string `json:"summary_selector,omitempty"` // item 内。省略可
    ...
}
```

| セレクタ | 必須 | 省略時の挙動 |
|---|---|---|
| `item_selector` | **必須** | — |
| `link_selector` | 任意 | item 自身が `<a href>` ならそれ。無ければ item の子孫で**文書順に最初の `<a href>`** |
| `title_selector` | 任意 | リンク要素のテキスト。空なら item 全体のテキストを 120 文字に切って使う |
| `date_selector` | 任意 | 一覧からは取らず、個別ページから取る（§4.6） |
| `summary_selector` | 任意 | 一覧からは抜粋を取らない |

- `item_selector` は**ページ全体に対する**セレクタ、残りは**各 item 要素に対する
  相対セレクタ**として評価する。`cascadia` はサブツリーに対する検索を
  そのまま提供するので、実装は素直に書ける。
- **リンクの取り出し方**: `link_selector` がマッチした要素が `<a>` なら `href`、
  そうでなければその子孫の最初の `<a href>`。`href` を持たない要素しか
  無い item はスキップする（記事として成立しないため）。
- **日付の取り出し方**: `date_selector` の要素が `<time datetime>` を持つなら
  その属性値、無ければテキスト。パースは §4.6 と同じ関数を使う。
  **一覧上の日付が取れれば個別ページの取得前に `published_at` が確定する**ので、
  §4.5 の持ち越し（1 回のクロールで取り切れない分）があっても
  時系列が壊れない。日付セレクタを指定する価値はここにある。
- `SummarySelector` は、`Config.Fulltext = false`（個別ページを取りに行かない設定、
  §4.5）のときにエントリー本文として使う。一覧に抜粋があるサイトなら
  **1 リクエストのまま読める本文が手に入る**。

**セレクタの検証**: API 受信時に `cascadia.Compile` を通し、失敗すれば 400。
セレクタ 1 本あたり 200 文字、合計 5 本までとする。

### 4.3 URL の正規化

`link_selector` から得た `href` を次の順で処理する。

1. **絶対化**: 一覧ページの URL を base に解決する。解決できないもの
   （`javascript:`、`mailto:`、空文字、パースエラー）は捨てる。
2. **スキーム制限**: `http` / `https` 以外は捨てる。
3. **フラグメント除去**: `#section` は落とす。`/a/#c1` と `/a/#c2` が
   別記事として 2 件湧くのを防ぐ。
4. **トラッキングパラメータ除去**: `utm_*`, `fbclid`, `gclid`, `_ga`,
   `mc_cid`, `mc_eid` を除去する（F0 §4.3 と同じ集合・同じ実装を共有する。
   §13 のとおり共通化する）。これをやらないと、一覧ページが計測パラメータを
   付け替えるだけで同じ記事が毎回「新着」になる。
5. **同一ホスト制限**: `Config.same_host_only`（**既定 true**）が真なら、
   一覧ページとホストが一致しない URL を捨てる。`www.` の有無は同一とみなす。
   セレクタで記事コンテナを指定していても、その中のリンクが外部
   （引用元・スポンサー）を指すことはあるので、既定の安全弁として残す。
6. **重複排除**: 同一 URL は文書順で先に出た方を残す。

上限: `item_selector` のマッチは **500 件**まで（超過分は文書順で切り捨て、
`State.truncated` を立てて UI に警告を出す）。

### 4.4 新着判定 — なぜ「URL の初出」だけで判定するのか

方針検討ドキュメントの未解決論点に「URL の初出だけで十分か、
並び順・日付要素も見るか。並び順が変わるサイトでは誤検知しやすい」とあるが、
**URL の初出のみで判定する**。理由:

- 「並び順で新着を判定する」方式こそが並び順の変化に弱い。URL の集合差分は
  並び順に**一切影響されない**ので、おすすめ順・人気順に切り替わっても誤検知しない。
- 一覧上の日付表示は形式がばらばら（`2026/08/20`、`8月20日`、`3日前`）で、
  パースの失敗が新着判定の失敗に直結する。日付は
  `published_at` を埋める材料に留め、**新着判定には使わない**。

**ページネーションを辿らない**のもこの判定と対になっている。1 ページ目だけを
見ていれば、新着は必ず 1 ページ目に現れる。過去記事の遡及取得は
「購読した時点より前の記事は読まない」という RSS 本来の挙動と一致しており、
機能として不足していない。

したがって state が持つべきものは「これまでに見た記事 URL の集合」だけになる（§6.1）。

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
一覧から取れた `summary_selector` の内容（あれば）を本文にする。
1 サイト 1 リクエストで済むのでサイトへの負荷は最小になる。
「サイトに負荷をかけたくない」「一覧に十分な抜粋がある」ときの選択肢。

**取得に失敗した記事の扱い**:

| 失敗 | 扱い |
|---|---|
| fetch 失敗 / 4xx / 5xx | エントリーを**作らない**。state の `seen` にも**入れない**（次回リトライ）。`pending` の失敗回数を +1 |
| 本文抽出失敗 / 短すぎ | エントリーを**タイトル＋リンク（＋一覧の抜粋）で作る**。`seen` に入れる（リトライしない） |
| 失敗回数が `maxArticleRetries`（3）に達した | 同上で作り、`seen` に入れて諦める |

fetch 失敗を `seen` に入れずリトライするのは、**GUID が URL ベース（§5.2）
だから安全にできる**設計。同じ記事を次回また取り込んでも
`UpsertEntries` は `ON CONFLICT(feed_id, guid) DO NOTHING` → UPDATE 経路に入り、
重複エントリーにならず、`newCount` にも数えられない（適応間隔が誤って縮まらない）。
一方で 404 の URL を永久にリトライすると無駄なので、3 回で諦める。

**初回クロールの扱い**: `state` が NULL の初回も、通常どおり
`max_items_per_crawl` 件（既定 20）を取り込む。一覧に 100 件並んでいても
21 件目以降は次回以降に持ち越される。pagewatch のように
「監視を開始しました」の 1 件を作る特別扱いは**しない** — 本方式は
初回から本物の記事が並ぶので、購読直後に空にならないという目的が自然に満たされる。

### 4.6 公開日時の決定

`published_at` を「取得時刻」にしてしまうと、一覧の下の方にある古い記事を
後から取り込んだときに**時系列が壊れる**（3 年前の記事が今日の記事として並ぶ）。
次の順で探す。

1. **一覧の `date_selector`**（指定されていれば最優先。個別ページを取る前に確定する）
2. `<meta property="article:published_time">`（OGP。ニュース系で普及率が高い）
3. JSON-LD（`application/ld+json`）の `datePublished`
   （`Article` / `NewsArticle` / `BlogPosting` のいずれか、最初に見つかったもの）
4. `<time datetime="...">` のうち文書順で最初のもの
5. いずれも無ければ **取得時刻**とし、`DateMissing` を立てる

パースは RFC3339 → `2006-01-02` → 日本語の `2006年1月2日` の順に試す。
未来日（現在時刻 + 1 日より先）は信用せず取得時刻にフォールバックする
（予約投稿のプレースホルダ対策）。

2〜4 の実装は `internal/extract/selector` ではなく
**`internal/fulltext` 側に `ExtractPublished(html) (time.Time, bool)` として置く**。
理由: 「個別記事ページから記事メタデータを取る」という同じ関心事であり、
DESIGN.md 未決事項 #1 の全文取得機能（実フィードの entry を全文化する経路）でも
「フィードに日付が無いが記事ページには日付がある」ケースで同じものが使えるため。
方式 B0（構造化データの活用）のうち、**費用対効果が明確な部分だけを
ここで先取りする**という位置づけでもある。

### 4.7 タイトルの決定

1. `title_selector`（指定されていればそのテキスト）
2. リンク要素のテキスト
3. 記事ページの Readability タイトル
4. 記事ページの `<title>`

テキストは NFKC 正規化 → 空白畳み込み → トリム（F0 §4.4 と同じ処理を共有）。
1・2 を 3・4 より優先するのは、一覧のリンクテキストが
**サイト運営者が「一覧で見せたい題」として書いたもの**であり、
`<title>` にありがちな「記事名 | サイト名」より読み流しに向くため。

## 5. エントリー生成

### 5.1 gofeed.Feed へのマッピング

| gofeed | 値 |
|---|---|
| `Feed.Title` | 一覧ページの `<title>`（空なら URL のホスト名）。F0 の `displayTitle` を共有 |
| `Feed.Link` | 一覧ページの URL |
| `Item.Title` | §4.7 |
| `Item.Link` | 記事の URL（正規化後） |
| `Item.GUID` | §5.2 |
| `Item.Content` | 抽出した本文。取れなければ一覧の抜粋、それも無ければ空 |
| `Item.PublishedParsed` | §4.6 |

### 5.2 GUID 設計

**GUID = 正規化後の記事 URL そのもの**（ハッシュ化しない）。

- F0 のような `hash(内容)` は不要。記事ごとに固有の URL があるという
  前提が本方式の出発点だから、URL がそのまま自然キーになる。
- ハッシュ化しないのは、DB を直接見たときに何の記事か分かる方が
  デバッグしやすいため。`entries.guid` は TEXT で長さ制約もない。
- 記事がリライトされて本文が変わっても GUID は変わらないので、
  `UpsertEntries` の UPDATE 経路で本文だけが差し替わる。既読状態は保たれる。
  これは RSS の挙動と同じで、望ましい。
- **URL が変わると別記事になる**（パーマリンク変更）。サイトのリニューアル時に
  一時的に重複エントリーが出るが、フィードを配信しているサイトでも
  `guid` を変えれば同じことが起きるので、方式固有の欠陥ではない。

### 5.3 エントリー本文

F0 のような差分表示は不要。本文はそのまま記事本文が入る。
本文も抜粋も取れなかった場合は**空**にする。UI 側は本文が空でも
タイトルとリンクで読み飛ばせるので、「本文を取得できませんでした」という
プレースホルダ文字列は入れない — 全文検索のインデックスを汚すため。

## 6. 永続化

### 6.1 state の中身（selector）

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
| `version` | state の JSON フォーマット版。想定外なら空 `seen` として扱う（§6.3） |
| `config_hash` | セレクタ群を正規化した JSON の SHA-256。記録目的のみ（§6.3） |
| `truncated` | 候補数上限で切り捨てたか（UI 警告用） |
| `seen` | 取り込み済み記事 URL。追加順で保持し、上限超過時は古い方から捨てる |
| `pending` | 取得に失敗中の URL → 連続失敗回数（§4.5） |

`rules_version`（F0 にある、feedla 側の除去規則の版）は**持たない**。
本方式の判定はユーザーが書いたセレクタだけで決まり、
feedla 側のハードコード規則に依存しないため。

**サイズ上限**: `seen` は **2000 件**（1 件 100 バイト前後として約 200KB）。
超過分は追加順の古い方から捨てる。捨てた URL が一覧ページにまだ載っている
場合は再度「新着」として取り込まれてしまうが、GUID が URL ベースなので
**重複エントリーにはならず、既読も保たれる**（UPDATE 経路）。
2000 件を超えて一覧に載り続ける記事があるサイトは事実上ない。

### 6.2 なぜ entries テーブルを既知 URL の集合として使わないのか

「`entries` に `url` があるのだから `SELECT 1 FROM entries WHERE feed_id=? AND url=?`
で既知判定すればよく、state は要らないのでは」という案を検討したが**採らない**。
F0 設計 §6.4 と同じ理由:

- `entries` は GC で 30 日で消える（DESIGN.md「GC / リテンション」）。
  消えた瞬間、一覧にまだ載っている古い記事が**全部「新着」として再取得される**。
  記事本文の再 fetch が N 件走り、サイトに無用な負荷をかける。
- **保持期間の異なるデータに依存してはいけない**。エントリーは読んだら
  消えてよいデータ、state は購読が続く限り消してはいけないデータ。
- `pending`（失敗リトライ回数）は entries には置き場所がない。

### 6.3 state の無効化 — セレクタを変えても seen を捨てない

無効化のトリガは 2 つ:

1. ユーザーがセレクタを変更した（`config_hash` 不一致）
2. state の JSON が壊れている / `version` が想定外

**1 では `seen` を捨てない。** これが F0 との重要な差。

- F0 の state は「前回のページ内容」なので、比較の前提が変われば
  内容そのものが無意味になる（だから捨てて再同期する）。
- 本方式の `seen` は「**取り込んだ記事 URL の履歴**」であり、
  セレクタを変えても履歴の意味は変わらない。捨てると、
  セレクタを 1 文字直しただけで**取り込み済みの記事が全部もう一度
  未読として湧く**（かつ本文の再 fetch が N 件走る）ことになり、
  F0 設計 §6.6 が避けようとしたのと同じ失敗になる。
  **セレクタは試行錯誤しながら直すもの**なので、直すたびに未読が湧くのでは
  F2 の GUI（何度もクリックし直す）と決定的に相性が悪い。
- `config_hash` の不一致は「セレクタが変わった」という**記録目的だけ**に使い、
  挙動は変えない（次回のクロールから新しいセレクタが効く、それだけ）。
- 2（壊れた state）の場合は `seen` を空として扱う。この場合に限り
  一覧の記事が再取り込みされるが、GUID が URL ベースなので
  **重複エントリーにはならず、既読も保たれる**（本文の再 fetch は走る）。

この「捨てても壊滅しない」性質は §5.2 の URL ベース GUID がもたらしたもので、
本方式の state 設計を F0 よりずっと緩くしている。

### 6.4 OPML・バックアップとの関係

F0 と同じ扱いを踏襲する。

- **OPML export**: `kind != "feed"` の購読は除外する。現状の実装は
  `s.Kind == "pagewatch"` の等値比較なので、**`!= "feed"` に一般化する**
  （§7.1 で kind が増えるため）。
- **OPML import**: `feed_url` が既知のスクレイプ接頭辞で始まる outline は無視する。
  現状の `strings.HasPrefix(f.FeedURL, crawler.ScrapePrefix)` を
  接頭辞リストに対する判定に一般化する。
- `GET /api/v1/scrape_sources` が全設定を JSON で返すので、
  セレクタ設定もそのままバックアップ対象に入る（実装変更不要）。
- **state はバックアップ JSON に出さない**（既存の `scrapeSourceView` が
  State を含まない設計をそのまま利用）。失われても §6.3 のとおり
  重複エントリーにはならない。

## 7. crawler への統合

### 7.1 疑似スキームを 2 つに増やす

`feeds.feed_url` の接頭辞に `selector:` を足す。

```go
const (
    PagewatchPrefix = "pagewatch:"
    SelectorPrefix  = "selector:"
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
var scrapePrefixes = map[string]extract.Kind{
    PagewatchPrefix: extract.KindPageWatch,
    SelectorPrefix:  extract.KindSelector,
}

// cutScrapePrefix returns (実URL, kind, true) for a scrape-backed feed_url.
func cutScrapePrefix(feedURL string) (string, extract.Kind, bool)
```

`store` 側は crawler を import できない（依存方向の制約）ため、
現状どおり接頭辞の文字列を SQL 用に複製する。**複製が 2 箇所に増えるので、
接頭辞の一致を検証するテストを `internal/crawler` 側に置く**
（crawler は store を import できるので、`store` が公開する接頭辞リストと
突き合わせられる）。

`crawlOne` の分岐は、既存の `isScrape` bool を kind に置き換える:

```go
target, kind, isScrape := cutScrapePrefix(f.FeedURL)
...
if isScrape {
    parsed, scrapeState, err = c.extractPage(ctx, f.ID, kind, target, fr, now)
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
// 現状 applyFulltext は
//   ・feed_fulltext の有無を見る
//   ・ExistingEntryGUIDs で新規だけに絞る
//   ・maxFulltextFetchesPerCrawl で上限を掛ける
//   ・extractEntryFulltext で本文を差し替える
// このうち下 3 つを fillEntryBodies(ctx, entries, limit) として切り出し、
// selector 経路からも呼ぶ。
```

ただし selector 経路では既存判定に `ExistingEntryGUIDs` を使わない
（state の `seen` が既に新着だけに絞っている）。また、
**失敗した URL を crawler が extract に伝え返す必要がある**（§4.5 の
`pending` 更新）。これを `extract.Result` の往復で表現すると
インターフェースに HTTP の匂いが漏れるので、次のようにする:

- `selector.Extract` は state を「今回の候補をすべて seen に入れた形」では返さない。
  代わりに `Result.State` には「前回の seen ＋ `pending` の更新なし」の状態を入れ、
  **新着候補は `Feed.Items` としてのみ返す**。
- crawler は取り込み結果（成功 URL の集合・失敗 URL の集合）を確定させたあと、
  `selector.CommitState(prevState, succeeded, failed) json.RawMessage`
  という**パッケージ公開のヘルパ**を呼んで最終 state を作る。
  この関数は純粋関数（HTTP を知らない）なので、依存方向の原則は保たれる。

`extract.Extractor` インターフェース自体は変えず、方式固有のヘルパを
方式のパッケージに置くだけなので、pagewatch 側には影響しない。

### 7.3 取得件数の上限と適応間隔

| 項目 | 値 | 根拠 |
|---|---|---|
| 1 クロールあたりの個別ページ取得 | `max_items_per_crawl`（既定 20、上限 50） | 既存の `maxFulltextFetchesPerCrawl` と同じ値 |
| 初期 `fetch_interval_sec` | 3600（1 時間） | pagewatch と同じ。通常フィードの 1800 より緩い |
| 最小間隔 | 既存の `MIN = 10min` | 適応制御の下限は共通 |

適応間隔（`nextIntervalOnSuccess`）は `newCount > 0` で駆動される既存ロジックを
そのまま使う。§4.5 のリトライ経路が UPDATE になり `newCount` に数えられないので、
**失敗リトライ中のサイトの間隔が誤って縮まることはない**。

### 7.4 robots.txt — 本方式では実装する

F0 設計 §10.3 は robots.txt を実装しない理由として
「対象はユーザーが明示的に 1 件ずつ登録した URL であり、**クローラによる
URL の自動発見・追跡がない**」ことを挙げ、そのうえで
「**Phase F1（一覧ページ + N 件の個別ページ取得）では取得件数が桁で増えるため、
そこで robots.txt 対応を必須要件として再検討する**」と申し送っている。

本方式はまさに「URL を自動発見して追跡する」機能なので、**実装する**。
F0 が挙げた不要理由が 1 つも成り立たない以上、
ここで実装しないなら申し送りを反故にする理由を別途示さねばならず、それは無い。

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
- 実装は `internal/crawler/robots.go` に自前で置く。外部依存を足さない —
  必要なのは `Disallow`/`Allow` の前方一致と `*`/`$` のワイルドカードだけ。
- Disallow された記事はエントリーを**タイトル＋リンクのみで作る**（本文なし）。
  **エントリー自体を作らないのは行き過ぎ**で、「その URL が存在する」ことは
  一覧ページ（ユーザーが読む権利のあるページ）に書いてある事実だから。

### 7.5 エラー分類

F0 設計 §7.4 の表に、本方式固有の行を足す。

| 事象 | 分類 | `error_count` | 備考 |
|---|---|---|---|
| 一覧ページの HTTP エラー / タイムアウト | external | ++ | 既存と同じ |
| 一覧ページの charset デコード / パース失敗 | external | ++ | 既存と同じ |
| **`item_selector` のマッチが 0 件** | external | ++ | セレクタ破損・サイト構造変更・bot ブロックの疑い。§9.3 で UI に出す |
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
POST   /api/v1/scrape_sources          {kind: "selector", url, folder_id?, title?, config}
GET    /api/v1/scrape_sources
GET    /api/v1/scrape_sources/{id}
PATCH  /api/v1/scrape_sources/{id}     {config}
POST   /api/v1/scrape_sources/{id}/preview
```

- `handleCreateScrapeSource` は現在 `kind != pagewatch` を 400 で弾いている。
  ここを許可リストに変え、kind ごとに config のバリデータ
  （`pagewatch.ParseConfig` / `selector.ParseConfig`）を切り替える。
- `feedURL := crawler.ScrapePrefix + req.URL` を kind に応じた接頭辞に変える。
- `handlePatchScrapeSource` も同様に、**保存済みの kind** に対応する
  バリデータを使う（リクエストの kind は見ない。kind の変更は許さない）。
- クオータ（`MaxScrapeSources`）・レート制限（`feedAddLimiter`・
  `previewLimiter`）・所有者チェック（作成者 or admin）は**すべて既存のまま流用**する。

### 8.2 購読前プレビュー（新規エンドポイント）

**セレクタを手で書く方式は、プレビューが無いと実用にならない。**
既存の preview は `{id}` を要求するため購読後にしか使えないので、
購読前に使えるものを足す:

```
POST /api/v1/scrape_sources/preview   {kind, url, config}  → 抽出結果
```

- レスポンスは kind ごとに形が違う。pagewatch は既存の `{blocks: [...]}`、
  selector は:

  ```json
  {
    "items": [
      {"url": "https://example.com/news/a", "title": "…", "date": "2026-08-20",
       "summary": "…", "seen": false}
    ],
    "matched": 18,
    "truncated": false,
    "warnings": ["3 件の要素にリンクが無く、スキップされました"]
  }
  ```

- `{id}/preview` の方も selector の場合はこの形を返す
  （保存済みの config で実行し、`seen` を state と突き合わせて埋める）。
  購読前プレビューでは `seen` は常に false。
- **`warnings`** は「セレクタは当たっているが取り出しに失敗した」ケースを
  ユーザーに見せるためのもの。「0 件です」だけだと `item_selector` が
  悪いのか `link_selector` が悪いのか切り分けられない。

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

### 8.3 F2 のための追加エンドポイント（本フェーズでは実装しない）

§10 の GUI は「ページの中身を安全に見せる」ために別のエンドポイントを要する。
F1 では実装しないが、**パスだけ予約しておく**（後から `/preview` の
レスポンスを拡張して兼用させると、F1 の API が肥大化するため）:

```
POST /api/v1/scrape_sources/inspect   {url}  → サニタイズ済み HTML + 要素インデックス
```

### 8.4 既存 API への影響

- `SubscriptionView.Kind` に `"selector"` が増える。SQL の CASE を
  2 分岐にする（§7.1）。**API の型は変わらない**（既に文字列）。
- `POST /api/v1/subscriptions`（通常の購読）は**変更しない**。
  フィード未検出時は従来どおり 502 を返し、UI が選択肢を出す（§9.1）。

## 9. Web UI（Phase F1）

### 9.1 登録導線

`AddSubscriptionDialog` の 502（フィード未検出）時の分岐に選択肢を 1 つ足す。
現在は「ページの更新を監視する」だけなので、次の 2 択にする:

```
このページにフィードが見つかりませんでした。

[記事一覧として取り込む]   一覧ページから記事を拾い、記事ごとに読めるようにします
[ページの更新を監視する]   ページ全体の変化を 1 件の記事として通知します（既存）
```

「記事一覧として取り込む」を選ぶと、同じダイアログ内で次のステップに進む:

1. **セレクタ入力**（`item_selector` は必須、残りは折りたたみの詳細設定）
2. **[プレビュー]** → `POST /scrape_sources/preview`。抽出された記事の
   タイトル・URL・日付を一覧表示し、`warnings` も出す
3. セレクタを直して 2 を繰り返す
4. **[この設定で購読]** → `POST /scrape_sources`

F2 が入ると、1 の位置に「[ページから選ぶ]」ボタンが増え、
**同じ Config を GUI で埋める**形になる（§10.4）。
ステップ 2〜4 は F2 でもそのまま使うので、この画面構成自体が F2 への布石になる。

登録時には pagewatch と同じ注意喚起を出す:
「サイト運営者はフィード配信を意図していません。一覧の取得は 1 時間に 1 回、
記事本文の取得は 1 回あたり最大 20 件です」。

### 9.2 一覧での区別

`SubscriptionTree` で `kind === 'selector'` の購読にアイコンを付ける
（pagewatch の 👁 と別のもの）。未読数・グループ・rating・pin・
全文検索は entries を共有しているのでそのまま動く。

### 9.3 抽出設定パネル

`FeedDetailOverlay` に、pagewatch の `PagewatchSettings` と並ぶ
`SelectorSettings` を追加する（kind で出し分け）:

- 一覧ページ URL（読み取り専用）
- 各セレクタの編集（保存すると PATCH）
- `same_host_only` / `fulltext` のトグル、`max_items_per_crawl` の数値入力
- **[いま取得して確認]** → `{id}/preview`。抽出結果と、取り込み済みか（`seen`）を表示
- **セレクタが壊れた警告**: `error_count > 0` かつ `last_error` が
  「マッチ 0 件」を含む場合に、
  「セレクタにマッチする記事が見つかりませんでした。サイトの構成が
  変わった可能性があります」と赤字で出し、プレビューへ誘導する。

**この警告は本方式で最も重要な UI 要素**である。CSS セレクタは
マークアップ変更で確実に壊れるので、「壊れたまま静かに 0 件を返し続ける」ことを
防ぐ導線がないと、購読が死んだことにユーザーが気づけない。

## 10. Phase F2: GUI クリック選択の設計見通し

本フェーズでは実装しないが、**F1 の設計が F2 を縛らないこと**を確認するために
方式だけ決めておく。

### 10.1 何が難しいのか

方針検討ドキュメントは B1 について
「取得元 HTML をそのまま iframe 表示すれば任意の script が動きかねないため、
サニタイズ・サンドボックス化・元サイトの CSS を壊さない程度の整形が要る。
DESIGN.md の CSP・bluemonday 方針と同様の慎重さが必要」と書いている。
つまり難所は**セレクタの生成**ではなく**第三者 HTML の安全な表示**にある。

現状の CSP は次のとおり（`internal/web/web.go`）:

```
default-src 'self'; img-src 'self' https: data:; script-src 'self'; frame-src https://www.instagram.com
```

### 10.2 表示方式の比較

| 方式 | 見た目の再現 | 外部への通信 | 実装コスト | 判定 |
|---|---|---|---|---|
| **A. sandbox iframe に srcdoc で流し込む（採用）** | 中（インライン style のみ効く） | **なし** | 中 | ◎ |
| B. A + 外部 CSS/画像を feedla 経由でプロキシ | 高 | feedla のサーバから発生 | 高（プロキシは新たな SSRF 面） | ✕ |
| C. 構造だけのアウトライン表示（タグとテキスト冒頭のツリー） | なし | なし | 低 | △ |
| D. サーバ側でスクリーンショット（headless） | 完全 | あり | 極大 | ✕（DESIGN.md の中核目標と衝突） |

**A を採用する。**

- `<iframe sandbox="allow-scripts" srcdoc="...">` に、
  **元ページの script を完全に除去した HTML** を入れる
  （`<script>`、`on*` 属性、`javascript:` URL、`<object>`/`<embed>` すべて）。
  `allow-scripts` が要るのはクリック検出のため（§10.3）。
  **`allow-same-origin` は付けない**ので、iframe は不透明オリジンになり、
  親の DOM・Cookie・`localStorage`・feedla の API セッションには一切到達できない。
  仮にサニタイズを擦り抜けた script があっても、
  できるのは「隔離された空オリジンの中で暴れる」ことだけになる。
- `<link rel=stylesheet>` / `<img src>` / `<iframe>` など**外部を参照する要素は
  すべて除去する**。CSP に穴を開けずに済むうえ、
  **プレビューを開いただけで第三者サイトに feedla ユーザーの IP と
  Referer が渡ることがない**（B を却下する決定的な理由もこれ）。
  画像は `alt` テキストのプレースホルダに置換する。
- インライン `style` 属性と `<style>` の中身は残す（外部参照を含む
  `@import` / `url()` は除去）。これだけでも段組みと大まかな見た目は保たれ、
  「どのブロックが記事一覧か」を目で判断するには十分。
- CSP は `frame-src` に `'self'` を足す必要がある（`srcdoc` の iframe は
  `frame-src` の評価対象）。Instagram の embed 用に既に `frame-src` があるので、
  値を 1 つ増やすだけで済む。

C（構造だけ）を採らないのは、**見た目がないと「どれが記事か」を
人間が判断できない**ため。それができるなら最初からセレクタを手で書ける。

### 10.3 クリックからセレクタを作る

`allow-same-origin` を付けない iframe は不透明オリジンになるため、
**親ウィンドウから `contentDocument` には触れない**。つまり
「クリックを親側だけで検出する」ことはできず、iframe の中で JS を動かす必要がある。
これが `sandbox="allow-scripts"` を付ける理由である。

構成:

1. `POST /scrape_sources/inspect`（§8.3）が、サニタイズ済み HTML を返す際に
   **各要素へ `data-feedla-id="N"` を振り**、あわせて要素インデックス
   （id → タグ名・class 集合・親子関係）を JSON で返す。
2. その HTML の末尾に、**feedla が書いた 1 本のスクリプト**を付けて `srcdoc` に流す。
   このスクリプトは click を拾って `data-feedla-id` を
   `parent.postMessage` で送るだけの数十行。
3. 親ウィンドウは `message` を受け、**インデックス（サーバ由来の構造化データ）**を
   使ってセレクタを組み立てる。iframe が送ってくるのは id という整数だけなので、
   **iframe 側から任意の文字列を親のロジックに流し込めない**。

安全性の根拠は 3 つ:

- 元ページの script は除去済み（サニタイズが主防御）
- `allow-same-origin` が無いので、iframe 内の JS は親にも feedla のセッションにも
  到達できない（オリジン隔離が第 2 の防御）
- iframe → 親の通信は「整数 1 個」に限定し、`event.origin` と
  ペイロードの型を親側で検証する（インターフェースの最小化が第 3 の防御）

**実装時に最初に検証すべき点**: `srcdoc` + `sandbox="allow-scripts"` の
iframe から `parent.postMessage` が届くこと、および注入スクリプトが
CSP の `script-src 'self'` に抵触しないこと（不透明オリジンの iframe には
親の CSP がどう適用されるかがブラウザ実装依存になりうる）。
F2 はここを 30 行のプロトタイプで確かめてから本実装に入る。

セレクタ生成のアルゴリズム（クリックされた要素 → `item_selector`）:

1. クリックされた要素から祖先を辿り、**同じ「構造シグネチャ」を持つ兄弟が
   3 個以上ある**最小の祖先を記事コンテナ候補とする。
   構造シグネチャ = タグ名 + class 集合（後述の不安定 class を除く）。
2. その候補を一意に指すセレクタを、次の優先順で作る:
   `tag.class`（class が全兄弟で共通なら）→ 親を 1 段足す → `nth-of-type` に落ちる。
3. **不安定な class を除外する**: 長い 16 進文字列、ランダムに見える
   英数字列（CSS Modules / CSS-in-JS のハッシュ）、数字のみ。
   これを入れないと、ビルドのたびに壊れるセレクタを生成してしまう。
4. 候補を 2〜3 個提示し、**それぞれで何件マッチするかを添えて**選ばせる。

`title_selector` 等は、記事コンテナを決めたあとに
「その中でタイトルらしき要素をクリック」→ コンテナからの相対セレクタを生成、
という同じ流れで作る。

### 10.4 F1 の設計が F2 を縛っていないことの確認

| F2 が必要とするもの | F1 での状態 |
|---|---|
| 保存する設定の形が同じ | ◎ `Config` の 5 セレクタをそのまま埋めるだけ |
| プレビューで結果を確認する画面 | ◎ §9.1 のステップ 2〜4 を共有 |
| セレクタを後から手直しできる | ◎ §9.3 の設定パネルがそのまま使える |
| 抽出・取得・エントリー化 | ◎ 一切変更なし |
| 追加で要るもの | `inspect` エンドポイント（§8.3 でパスだけ予約）、CSP の `frame-src 'self'`、iframe とセレクタ生成の UI |

**F2 は「Config を埋める別の入力手段」に閉じており、F1 の後方に何も要求しない。**

## 11. 設定・上限・礼儀

### 11.1 Config の全体像とデフォルト値

```go
type Config struct {
    ItemSelector     string `json:"item_selector"`                  // 必須
    LinkSelector     string `json:"link_selector,omitempty"`
    TitleSelector    string `json:"title_selector,omitempty"`
    DateSelector     string `json:"date_selector,omitempty"`
    SummarySelector  string `json:"summary_selector,omitempty"`
    SameHostOnly     *bool  `json:"same_host_only,omitempty"`       // 既定 true
    Fulltext         *bool  `json:"fulltext,omitempty"`             // 既定 true
    MaxItemsPerCrawl int    `json:"max_items_per_crawl,omitempty"`  // 既定 20、上限 50
}
```

| 項目 | 値 | 備考 |
|---|---|---|
| セレクタ 1 本の長さ | 最大 200 文字 | |
| `item_selector` のマッチ | 500 件 | 超過は切り捨て + `truncated` |
| 1 クロールの個別取得 | 20（上限 50） | §7.3 |
| `seen` 件数 | 2000 | §6.1 |
| 記事 1 本の失敗リトライ | 3 回 | §4.5 |
| robots.txt キャッシュ | 24 時間 | §7.4 |
| レスポンス上限 | 既存の 10 MiB | Fetcher 側 |

セレクタは API 受信時に `cascadia.Compile` を通して 400 で弾く
（F0 の `ignore_patterns` に対する `regexp.Compile` と同じ扱い）。

### 11.2 既存の礼儀機構で足りるか

`HostSemaphore`（同一ホスト同時 2・最低 1 秒間隔）が個別記事の取得にも
そのまま効くので、20 件の取得は最短でも約 10 秒かけて行われる。
「1 サイトに 20 リクエストを一瞬で叩き込む」ことにはならない。

**追加する礼儀は robots.txt（§7.4）だけ**。

### 11.3 著作権・利用規約

F0 §13 と同じ整理:

- 取得したページを手元の DB に保存するのは私的利用の範囲。
  登録時の注意喚起（§9.1）に留め、それ以上の制限は設けない。
- ただし本方式は**記事本文を丸ごと保存する**点で F0 より踏み込んでいる。
  `Config.fulltext = false`（一覧の抜粋のみ）という逃げ道を用意してあることを、
  設定 UI の説明文で明示する。
- **実在サイトの HTML をリポジトリに置かない**線は維持する。
  テストフィクスチャは `tools/htmlskeleton`（F0 で作成済み）で
  匿名化したものを使う。ただし**このツールは class/id をそのまま残す**仕様
  （F0 §14.5）なので、セレクタのテストにそのまま使える。

## 12. テスト計画

| 層 | 内容 |
|---|---|
| `extract/selector` 単体 | 一覧 HTML → 期待アイテム列。DB も HTTP も使わない。<br>・`item_selector` が複数マッチし、各 item から相対セレクタで link/title/date を取れること<br>・`link_selector` 省略時に item 内の最初の `<a href>` が使われること<br>・item 自身が `<a>` の場合<br>・リンクの無い item がスキップされ `warnings` に出ること<br>・相対 URL が一覧ページ基準で絶対化されること<br>・`utm_*` 付きとフラグメント違いが同一 URL に畳まれること<br>・外部ホストが `same_host_only` で落ちること／false で残ること<br>・500 件超で `truncated` が立つこと<br>・不正なセレクタが `ParseConfig` で弾かれること |
| state 単体 | `CommitState` が成功 URL のみ `seen` に入れ、失敗 URL の `pending` を +1 すること。3 回で諦めて `seen` に入ること。`seen` 2000 件超で古い方から捨てられること。**セレクタ変更（config_hash 不一致）で `seen` が捨てられないこと**。壊れた JSON でパニックせず空 `seen` に落ちること |
| 日付抽出単体 | `internal/fulltext.ExtractPublished`: OGP / JSON-LD / `<time>` の優先順位。未来日を弾くこと。一覧の `date_selector` が最優先されること |
| robots.txt 単体 | `Disallow`/`Allow` の最長一致、`*`/`$`、UA の選択、404/5xx は許可扱い、キャッシュが 24 時間効くこと |
| crawler 統合 | `httptest.Server` に一覧＋記事 3 本を置き、<br>・初回で 3 件、2 回目で 0 件<br>・一覧に 1 本増えたら 1 件（既存 3 本が再 fetch されないことをリクエスト数で検証）<br>・記事 1 本が 500 → その記事だけ未取り込み、次回リトライされること<br>・3 回失敗後はタイトルのみで取り込まれ、以後 fetch されないこと<br>・`max_items_per_crawl` を 2 にすると 1 回で 2 本だけ取ること<br>・記事が `Disallow` 配下なら本文なしで取り込まれること |
| store | `scrape_sources` の kind が `selector` でも CRUD/CASCADE が動くこと |
| 接頭辞の整合 | `crawler` の接頭辞リストと `store` の SQL 用定数が一致すること（§7.1） |
| API | `POST /scrape_sources {kind:"selector"}` → `GET /subscriptions` に `kind:"selector"` で出る。不正なセレクタ・空 `item_selector` が 400。`POST /scrape_sources/preview` が items を返す |
| **IDOR**（CLAUDE.md 必須） | ・他ユーザーの scrape source を PATCH できないこと<br>・`{id}/preview` を非所有者が叩けないこと<br>・`POST /scrape_sources/preview`（所有権チェック不能）が未認証で 401、レート制限が user 単位で効くこと |
| OPML | `selector:` 接頭辞の購読が export されず、import でも無視されること |
| e2e | 一覧ページのフィクスチャを 1 件購読 → 記事が複数出る → **末尾で必ず購読解除**（e2e スイートは DB とサイドバーを共有しており、残すと後続 spec の順序を壊す） |

## 13. 作業分解

| # | 作業 | 依存 | 完了条件 |
|---|---|---|---|
| 1 | `cascadia` を直接依存に昇格（`go.mod` の indirect 解除） | — | `go mod tidy` が安定 |
| 2 | `internal/extract/selector`: セレクタ適用・URL 正規化・アイテム生成 + `Config`/`ParseConfig` | 1 | 単体テスト緑（DB・HTTP 不要） |
| 3 | 同: `State` と `CommitState`（seen/pending の更新規則） | 2 | 単体テスト緑 |
| 4 | `internal/fulltext.ExtractPublished`（OGP / JSON-LD / `<time>`） | — | 単体テスト緑 |
| 5 | `internal/crawler/robots.go` + 24h キャッシュ | — | 単体テスト緑 |
| 6 | crawler: 接頭辞レジストリ化（`ScrapePrefix` → `PagewatchPrefix` + `SelectorPrefix`）、`extractPage` の kind ディスパッチ、store 側 SQL の CASE 追加 | — | 既存テスト緑のまま、pagewatch の挙動が変わらないこと |
| 7 | crawler: 個別ページ取得ループ（`applyFulltext` からの共通部切り出し、上限・リトライ・robots 判定・state コミット） | 2〜6 | crawler 統合テスト緑 |
| 8 | API: kind 一般化（create/patch のバリデータ切り替え）+ `POST /scrape_sources/preview` | 6,7 | API テスト + IDOR テスト緑 |
| 9 | Web UI: 登録導線の 2 択化、セレクタ入力＋プレビュー、`SelectorSettings`、アイコン | 8 | 手動確認 + e2e |
| 10 | OPML export/import の `kind != "feed"` 一般化 | 6 | round-trip テスト緑 |
| 11 | 共通化: URL 正規化（トラッキングパラメータ除去・絶対化）を pagewatch と共有 | 2 | pagewatch の golden テストが変わらないこと |
| 12 | ドキュメント更新（DESIGN.md、方針検討ドキュメント、README） | 9 | — |
| — | **以降は Phase F2**（§10）: `inspect` エンドポイント、CSP の `frame-src 'self'`、iframe プレビューとクリック→セレクタ生成 | 9 | 別 PR |

**Phase F1 の受け入れ条件**: フィードを配信していない一覧ページを 2 種類
（記事カードが `<article>` で区切られた形式／`<li>` の羅列形式）購読し、
新着が記事単位のエントリーとして読めること。判定は匿名化フィクスチャによる
ゴールデンテストを主、実運用観察を従とする（F0 §14.3 と同じ方針）。

## 14. 既知の制約・持ち越す論点

- **F1 単体ではセレクタが書けないと使えない**。これは F1 の到達点を
  意図的にそこに置いた結果であり、F2（§10）で解消する。
  F1 のプレビューは「書けるが自信がない」層までしか救わない。
- **セレクタはマークアップ変更で壊れる**。壊れたことは「マッチ 0 件」の
  external エラーとして観測でき、§9.3 の警告でユーザーに届く。
  自動復旧はしない（B2 的な再推測は F3 の領域）。
- **一覧の 1 ページ目しか見ない**（§4.4）。購読開始より前の記事は
  `max_items_per_crawl` の持ち越し分を除いて取り込まれない。
- **記事の更新を検知しない**。一度 `seen` に入った URL は再取得しない。
  記事が後から加筆されても本文は初回取得時のまま。1+N のコストを抑える
  ための意図的な割り切りで、必要になれば「`seen` に `Last-Modified` を
  持たせて条件付き GET で再取得」を後から足せる（`State.version` を上げる）。
- **SPA の一覧ページは取れない**（方式 E、恒久的に対象外）。
  素の GET でセレクタが 0 件になり、§7.5 のエラーとして
  セレクタ破損と同じ見え方で出る。両者の区別は、プレビューが
  「ページは取れているが 0 件」なのか「そもそもページが空」なのかを
  示すことで与える。
- **bot 対策サイトは対応できない**。回避策は講じず
  「対応できないサイトがある」ことを前提として明示する。
- **F2 の iframe とセレクタ生成の実現性は未検証**（§10.3）。
  `sandbox` 属性とクリック検出の組み合わせは、F2 の実装時に
  **最初に小さく検証する**べき箇所として明記しておく。
- **方式 B0（sitemap.xml / JSON-LD からの一覧取得）は含めない**。
  §4.6 で「記事ページから公開日時を取る」部分だけ先取りする。
- **`internal/extract` 配下でのコード共有**: URL 正規化
  （トラッキングパラメータ除去・絶対化）とテキスト正規化（NFKC・空白畳み込み）は
  pagewatch の `attrs.go` / `block.go` に既にある。方針検討ドキュメントが
  認めているとおり `internal/extract` 内でのパッケージ間再利用は許容されるので、
  共通の場所に切り出して両方から使う（作業分解 #11）。
