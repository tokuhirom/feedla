# 方式 A: 単一ページ監視（pagewatch）詳細設計

ステータス: **設計案（未実装）**。
[フィード非提供サイトの購読機能 — 方針検討](feedless-site-subscription.md) の
**Phase F0（方式 A）** の実装設計。全体方針・他方式との比較はそちらを参照。

## 1. スコープ

**やること**: ユーザーが登録した 1 つの HTML ページを定期取得し、
HTML をパースして「ノイズノード」を落としたうえで前回取得分との差分を取り、
差分があればエントリーを 1 件生成して既存の未読管理に流す。

**やらないこと（F0 の非スコープ）**:

- 一覧ページから記事を複数拾う（方式 B / Phase F1 以降）
- Readability 相当の「本文らしさスコアリング」による本文抽出
- CSS セレクタによる監視範囲指定（Config に予約枠だけ用意し、実装は将来）
- JS レンダリング、robots.txt の取得・解釈（後述 §10.3 に理由）

**対象**: 「1 URL の中身が書き換わる」タイプすべて。具体的には

- **フィードを配信していない個人サイトの日記**（最新 N 件が 1 ページに載る形式）
  — **MVP はここ**。実在の 2 サイトで検証する（§14）
- 更新日を持たないお知らせページ、障害情報・ステータスページ、FAQ、規約ページ
  （方針検討ドキュメントの対象サイト例パターン 5）

当初はパターン 5 を主眼に置いていたが、MVP 対象の実測（§14）から
**前者の方が実用上の主戦場**であることが分かったため、既定値の選択
（`watch_mode`、§4.7）は前者に最適化してある。

## 2. 全体像

```
 Scheduler(既存 30s tick)
   └─ ClaimDueFeeds ──▶ crawlOne(feed)
                          │
                          ├─ feed_url が "pagewatch:" 接頭辞か？
                          │     no ─▶ 既存の ParseFeed 経路（変更なし）
                          │     yes
                          ▼
                  Fetcher.Fetch(実URL, Accept: text/html)   ← 条件付きGETは既存のまま
                          │ 304 → 何もせず interval 更新（最安）
                          ▼ 200
                  charset デコード（Content-Type / <meta charset>）
                          │
                          ▼
            internal/extract/pagewatch.Extract(html, prevState)  ← prevState = scrape_sources.state
                  ├─ x/net/html でパース
                  ├─ ノイズノード除去（ハードコード規則）
                  ├─ 属性フィルタ
                  ├─ ブロック分割 → []Block{Key, HTML}
                  ├─ ignore_patterns でマスク（比較キーのみ）
                  └─ 前回 Block 列と LCS 差分
                          │
                          │ 差分なし → entries 0 件（＝新規なし扱い）
                          ▼ 差分あり
                  *gofeed.Feed（1 item: タイトル/本文=差分HTML/GUID）
                          │
                          ▼
            既存 crawler.normalizeItem → store.UpsertEntries（無変更）
                          │
                          ▼
            scrape_sources.state を今回の Block 列で更新
```

**設計の要**: 抽出結果を `*gofeed.Feed` に落とすことで、
サニタイズ・本文長切り詰め・`EntryInput` 化・`UpsertEntries` という
既存の書き込みパイプラインを一切変更せずに再利用する。

## 3. パッケージ構成と依存方向

```
internal/extract/            共通型（Extractor, Input, Result）。DB もHTTPも知らない
  pagewatch/                 方式Aの実装。x/net/html だけに依存
internal/crawler/            → internal/extract を import（逆方向は不可）
internal/store/              scrape_sources の CRUD。extract を import しない
```

- `internal/extract` は `internal/store` / `internal/crawler` を import しない。
  一方向依存（crawler → extract）を golangci-lint の `depguard` で固定してもよい。
- **新規の外部依存はゼロ**。`golang.org/x/net/html`（既に `internal/feed/discover.go`
  が使用）と `golang.org/x/net/html/charset`、`golang.org/x/text`（既に依存）で足りる。
  `go-shiori/go-readability` は §4.1 の理由により **不採用**。

### 3.1 共通インターフェース

```go
package extract

type Kind string

const KindPageWatch Kind = "pagewatch"

// Input は「今回取得したページ」と「前回の抽出状態」。
type Input struct {
    URL         string          // 実URL（pagewatch: 接頭辞を外したもの）
    HTML        []byte          // UTF-8 に正規化済みの HTML
    ContentType string
    Now         time.Time
    Config      json.RawMessage // 方式ごとの設定（scrape_sources.config）
    State       json.RawMessage // 前回 Extract が返した State（初回は nil）
}

// Result は合成した疑似フィードと、次回に渡す不透明な状態。
type Result struct {
    Feed  *gofeed.Feed    // items が 0 件 = 変化なし
    State json.RawMessage // nil なら state を更新しない
}

type Extractor interface {
    Extract(ctx context.Context, in Input) (*Result, error)
}
```

- **State を不透明な JSON にする**のが要点。store 側は `scrape_sources.state` という
  1 カラムを持つだけでよく、方式が増えても列が増えない。
  Phase F1（`urlpattern`）は「既知 URL 集合」を、F2 は「セレクタの当たり状況」を
  同じ枠に入れられる。
- `Config`/`State` を `json.RawMessage` にすることで、`internal/store` は
  中身のスキーマを知らずに読み書きできる（型の知識は extract 側に閉じる）。

## 4. 抽出パイプライン（pagewatch）

### 4.1 なぜ Readability ではなくノード除去なのか

| | Readability 型（本文スコアリング） | ノイズノード除去（採用） |
|---|---|---|
| 目的 | 「本文はどこか」を当てる | 「本文でない所」を落とす |
| 対象ページ | 記事ページ | **お知らせ一覧・ステータス表など、本文らしい連続文がないページ** |
| 誤りの影響 | 本文領域を外すと差分が丸ごと消える／湧く | 落とし残しは ignore_patterns で潰せる |
| 依存 | 外部ライブラリ 1 つ（+ 推移依存） | 追加ゼロ |

方式 A の監視対象は「表・箇条書き・カード」が主で、Readability が想定する
「長い散文の本文ブロック」が存在しないことが多い。スコアリングは
**ページ構造が少し変わるだけで採択領域が別ブロックに飛ぶ**性質があり、
差分検知の入力としては不安定（ページ全体が入れ替わったように見える）。
ExtractContent 系の「本文以外を除外する」逆転の発想（方針検討ドキュメント参照）に
寄せ、**除去規則をハードコードして決定的に動かす**方を採る。

なお、この選択により DESIGN.md 未決事項 #1（フィード内の全文取得）との共有は
「HTTP 取得・charset 判定・サニタイズ」層に留まる。本文抽出そのものが必要に
なった時点で、Readability 相当を別パッケージとして足せばよい。

### 4.2 ノイズノード除去（ハードコード規則）

`x/net/html` の DOM を走査し、以下に該当するノードを **サブツリーごと** 削除する。

**(a) タグ名で落とす**

```
script, style, noscript, template, svg, canvas, iframe, object, embed,
form, input, button, select, textarea, label,
nav, header, footer, aside
```

- コメントノード（`html.CommentNode`）も全削除。
- `header`/`footer` はページ内の記事カード内部で使われている場合もあるが、
  **`<body>` 直下から数えて深さ 3 以内のもののみ削除**する（サイト全体の
  ヘッダ・フッタを狙い、カード内 `<header>` を巻き込まない）。深さのしきい値は
  定数 1 つで調整可能にしておく。

**(b) ARIA ロール / landmark で落とす**

```
role = banner, navigation, contentinfo, complementary, search, dialog, alert
```

**(c) id / class の語で落とす**

`id`・`class` の値を `[-_\s]` で語分割し、次の語を **完全一致** で含むものを削除:

```
nav, navi, navigation, menu, gnav, gnavi, breadcrumb, breadcrumbs,
sidebar, side, aside, footer, header, banner, ad, ads, advertisement,
sns, share, social, comment, comments, related, recommend, ranking,
pagination, pager, cookie, cookiebar, gotop, pagetop, skip,
lastmod, lastmodified, docid, counter, access
```

- 末尾 1 行は「**ページを再生成しただけで変わる自己言及的な要素**」。
  `<div class="lastmod">Last Modified: ...</div>` や
  `<div class="docid">Document ID: 3f2a...</div>` は本文が 1 文字も変わって
  いなくても差分を生む、最も質の悪いノイズ源で、実際に MVP 対象の 1 つが
  両方を持っている（§14.2）。
- 逆に `timestamp` / `date` / `time` は **除去語に入れない**。記事の日付は
  本文の一部であり、`<span class="timestamp">` が消えると「いつの記事か」が
  分からなくなる。「毎回変わる日付」と「記事の日付」はクラス名では
  区別できないので、前者は `ignore_patterns` で個別に潰す。

- 部分一致（`strings.Contains`）ではなく **語の完全一致**にするのは、
  `header-image` は落としたいが `sub-headers-of-contents` のような語を
  巻き込みたくないため。誤爆したときはユーザー側で救済できない
  （UI から「このブロックを戻す」導線は F0 にない）ので保守的に倒す。
- 落とし残しは `ignore_patterns`（§4.5）でユーザーが潰せる。逆に
  **落としすぎは救済手段がない**ので、迷ったら落とさない。

**(d) 非表示の指定**

`hidden` 属性、`style` に `display:none` / `visibility:hidden` を含むもの、
`aria-hidden="true"`。

### 4.3 属性フィルタ

残ったノードの属性は **許可制**で絞る。差分の意味に関わる属性だけを残し、
見た目・計測・フレームワーク由来の属性は落とす。

| 要素 | 残す属性 |
|---|---|
| `a` | `href` |
| `img` | `src`, `alt` |
| `time` | `datetime` |
| その他 | なし（すべて削除） |

- `class` / `id` / `style` / `data-*` / `on*` / `srcset` / `width` / `height` は削除。
  これらは CDN のハッシュ付きクラス名やビルドごとに変わる ID を含みやすく、
  **残すと毎回差分ありになる**。
- `href`/`src` は §4.4 のブロック生成前に監視対象 URL を base として絶対化する。
  相対 URL のまま比較すると、サイトが絶対 URL 表記に切り替えただけで全差分になる。
- `href` にクエリのトラッキングパラメータ（`utm_*`, `fbclid`, `gclid`,
  `_ga`, `mc_cid`, `mc_eid`）が付いている場合は除去してから比較する。

### 4.4 ブロック分割

差分の粒度を決めるため、正規化後の DOM を **ブロックの列**に平坦化する。

- ブロック要素 = `p, li, tr, dt, dd, h1..h6, blockquote, pre, figcaption,
  td, th, section, article, div` のうち、
  **「テキストを持ち、かつ同じ集合のブロック要素を子孫に持たない」最小単位**。
  （`div` の入れ子でも、実際に文字を含む最内周の 1 つだけがブロックになる）
- どのブロック要素にも属さないテキスト（`<body>` 直下の裸テキスト等）は、
  直前の兄弟までをまとめて 1 ブロックとして扱う。
- 各ブロックについて次を作る:

```go
type Block struct {
    HTML   string // 表示用。正規化済み HTML 断片（サニタイズは後段）
    Text   string // タグを除いたテキスト（空白畳み込み・NFKC 済み）
    Key    string // 比較キー。§4.5 のマスク適用後の HTML
    Anchor string // 直前の見出し（h1..h6）が持つ id/name、なければ ""
    Head   string // 直前の見出しのテキスト
}
```

`Anchor` / `Head` は F0 では**記録するだけで使わない**。MVP 対象の 2 サイトは
どちらも `<h2><a id="202608120735">…</a></h2>` のように記事ごとの安定した
アンカーを持っており（§14）、「追加ブロック列を見出し単位で切って
記事ごとのエントリーにする」拡張（F0.5、§14.4）の材料になる。
**state のフォーマットに関わるので F0 の時点で持たせておく**
（後から足すと全 state が再同期になる）。

- `Text` が空（画像のみ等）で `HTML` に `img`/`a` も含まないブロックは捨てる。
- テキストの正規化: NFKC → 全角空白を半角に → 連続空白を 1 個に →
  行頭行末トリム。`&nbsp;` は空白として扱う。
- ブロック数の上限 **5000**。超過分は切り捨て、`State` にフラグを立てて
  UI に「ページが大きすぎるため一部のみ監視しています」と出す。

### 4.5 ignore_patterns（ユーザーによるノイズ除去）

`Config.ignore_patterns` は Go の正規表現の配列。各ブロックの
**比較キー生成時にのみ**適用する（表示用 `HTML` は元のまま）。

1. ブロックの正規化 HTML に対し、各パターンのマッチ部分を空文字に置換
2. 置換後のテキストが空になったブロックは **比較対象から除外**
3. 残りを `Key` とする

これで「毎回変わる日時表示」「アクセスカウンタ」「ランダムな広告枠 ID」を
ユーザーが後から潰せる。パターンは
`ReadOnly` にコンパイルしてキャッシュし、不正な正規表現は
`scrape_sources` 更新時に API 側で弾く（`regexp.Compile` を通す）。

`Config.min_change_chars`（既定 0 = 無効）: 追加＋削除ブロックのテキスト合計が
この文字数未満なら「変化なし」として扱う。細かい表記ゆれで通知が飛ぶのを抑える
最後の砦。

### 4.6 差分判定

- ページ全体のハッシュ = 全ブロックの `Key` を `\n` 連結した SHA-256。
  前回 `State.content_hash` と一致すれば **即座に「変化なし」**（LCS を回さない）。
- 不一致なら前回 Block 列と今回 Block 列で **行単位 LCS**（`Key` を要素とする）
  を取り、`Added []Block` / `Removed []Block` を得る。
  - 実装は標準ライブラリのみの LCS（DP）。両側 5000 ブロック上限なので
    最悪 25M セルになる → **どちらかが 2000 ブロックを超える場合は LCS をやめ、
    集合差分（`Key` の multiset 差）にフォールバック**する。順序変更は検出できなく
    なるが、メモリと時間の上限を保証する方を優先する。
  - 「移動しただけ（順序入れ替え）」は集合差分では差分ゼロになる。これは
    誤検知抑制として望ましい方向なので、LCS 経路でも
    **追加集合と削除集合の両方に現れる `Key` は打ち消す**。
- `Added` も `Removed` も空なら items 0 件（＝新規なし）。

### 4.7 watch_mode — 削除を無視するか

`Config.watch_mode`:

| 値 | 挙動 | 向いているページ |
|---|---|---|
| `"additions"`（**既定**） | `Added` のみをエントリーにする。`Removed` は state 更新のみで通知しない。`Added` が空なら「変化なし」扱い | 日記・お知らせ一覧など、**最新 N 件だけを載せるローリング表示** |
| `"changes"` | `Added` と `Removed` の両方をエントリーに出す | ステータスページ・規約ページなど、**消えたことにも意味がある**ページ |

**既定を `additions` にするのは MVP（§14）の実測から**。最新 N 日分だけを
載せるページでは、新しい記事が入るたびに古い記事がページ末尾から押し出される。
`changes` だと「削除された記事本文」が毎回 `<del>` で大量に出て、
**本来読みたい新着より削除ノイズの方が長くなる**。

`additions` でも state は今回のブロック列で更新する（押し出された記事を
次回また「削除」として見ないため）。§7.3 の「変化がなければ書かない」の
基準は `content_hash` であり、`Added` が空でも `Removed` があれば
state は書き換わる。

## 5. エントリー生成

### 5.1 gofeed.Feed へのマッピング

| gofeed | 値 |
|---|---|
| `Feed.Title` | ページの `<title>`（空なら URL のホスト名） |
| `Feed.Link` | 監視対象 URL |
| `Item.Title` | `{ページタイトル} — 更新 (01/02 15:04)`（検知時刻、ローカルタイム） |
| `Item.Link` | 監視対象 URL |
| `Item.GUID` | §5.3 |
| `Item.Content` | §5.2 の差分 HTML |
| `Item.PublishedParsed` | 検知時刻（`Input.Now`） |

`Item.PublishedParsed` を必ず埋めるため、`crawler.normalizeItem` の
`DateMissing` は false になる。`UpsertEntries` のバックログ抑止ロジック
（日付なしフィードは先頭 1 件だけ未読）には影響しない。

### 5.2 エントリー本文

```html
<p>3 ブロック追加 / 1 ブロック削除</p>
<ins>
  <p>2026-08-16 メンテナンスのお知らせ</p>
  <p>8/20 2:00-4:00 にメンテナンスを実施します。</p>
</ins>
<del>
  <p>現在、障害は発生していません。</p>
</del>
<hr>
<p>ページ全文（監視対象部分）</p>
<div> ... 今回の正規化済み HTML 全体 ... </div>
```

- `<ins>` / `<del>` / `<hr>` は `bluemonday.UGCPolicy()` を通しても残る要素を選ぶ。
  **既存の `crawler.bodyPolicy` を通した結果が期待どおりであることを
  ユニットテストで固定する**（ポリシー変更で差分表示が崩れるのを検知するため）。
- 本文は既存パイプラインの 512KiB 切り詰めに従う。全文セクションが大きい場合は
  切られるが、差分を先頭に置いてあるので重要な情報は残る。
- 全文セクションを付けるのは、差分だけでは文脈が分からないページ
  （表の 1 セルだけ変わった等）への保険。`Config.include_full_body`（既定 true）で
  切れるようにする。

### 5.3 GUID 設計

既存の `UNIQUE(feed_id, guid)` に素直に乗せるため、
**GUID = `hex(sha256(今回のページ全体の content_hash))`** とする（内容ベース）。

- 内容が A → B → A と往復した場合、3 回目は 1 回目と同じ GUID になり
  **新規エントリーにならない**（`UpsertEntries` の UPDATE 経路に入り、
  既読状態も保持される）。ステータスページの「障害 → 復旧 → 障害」を
  別エントリーにしたい場合は不都合だが、**フラッピングで未読が無限に湧く方が
  実害が大きい**ためこちらを既定にする。
- 将来 `Config.guid_mode: "content" | "revision"` で切り替えられるよう
  フィールドだけ用意する（`revision` は `hash(url + 連番)` で毎回新規）。
- **内容ベース GUID には、state 更新の耐障害性という副次的な利点がある**（§7.3）。

## 6. 永続化

### 6.1 マイグレーション `0004_scrape_sources.sql`

```sql
CREATE TABLE scrape_sources (
  id          INTEGER PRIMARY KEY,
  feed_id     INTEGER NOT NULL UNIQUE REFERENCES feeds(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL,              -- 'pagewatch'（将来 'urlpattern' 等）
  target_url  TEXT    NOT NULL,              -- 実際に取得する URL（http/https）
  config      TEXT    NOT NULL DEFAULT '{}', -- 方式固有の設定（JSON）
  state       TEXT,                          -- 前回抽出の不透明な状態（JSON）
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
```

- `feeds` / `subscriptions` には **カラムを 1 つも足さない**
  （方針検討ドキュメントの「方式が増えるたびにコアのスキーマが太る」回避）。
- `feed_id UNIQUE` なので専用インデックスは不要。
- `ON DELETE CASCADE` により、購読解除（`DeleteFeed`）で設定も消える。

### 6.2 feeds 行との紐付け — 疑似スキーム

`feeds.feed_url` に **`pagewatch:https://example.com/status` という疑似 URL** を入れる。

なぜ `feed_url` に実 URL を入れて `scrape_sources` との JOIN で判定しないのか:

| | 疑似スキーム（採用） | JOIN で判定 |
|---|---|---|
| `ClaimDueFeeds` / `Feed` 型 | **無変更** | LEFT JOIN と列追加が要る |
| 重複登録の防止 | `UNIQUE(feed_url)` がそのまま効く | 別途 UNIQUE が要る |
| 誤って通常経路に流れたとき | `Fetcher` の scheme 検査で安全に失敗 | HTML を feed としてパースし失敗 |
| OPML に混入したとき | `xmlUrl` が明らかに異物と分かる | 実 URL なので気づけない |

デメリットは「`feed_url` が人間向けの URL でなくなる」点だが、API が
`SubscriptionView` を返す際に接頭辞を外した実 URL と `kind` を返すことで
UI 側には露出させない（§8.2）。

### 6.3 state の中身（pagewatch）

差分を取るには前回の取得結果を保持しておく必要がある。これを
`scrape_sources.state`（JSON）に置く。

```json
{
  "version": 1,
  "rules_version": 1,
  "config_hash": "3ab1…",
  "content_hash": "9f2b…",
  "truncated": false,
  "blocks": [{"k": "比較キー", "h": "<p>表示用HTML</p>", "a": "202608120735", "t": "きょうは"}]
}
```

| フィールド | 用途 |
|---|---|
| `version` | state の JSON フォーマット版。想定外なら state を捨てて再同期（§6.6） |
| `rules_version` | §4.2 の除去規則の版。feedla 側の規則を変えたら再同期が要る |
| `config_hash` | `ignore_patterns` 等を正規化した JSON の SHA-256。変わったら再同期 |
| `content_hash` | 全ブロックの `k` を連結した SHA-256。§4.6 の早期判定に使う |
| `truncated` | ブロック数上限で切り捨てたか（UI 警告用） |
| `blocks` | 前回のブロック列。`k` = 比較キー、`h` = 表示用 HTML、`a` = 見出しアンカー、`t` = 見出しテキスト |

- 「最後に確認した時刻」は state に持たない。`feeds.last_success_at` /
  `last_fetched_at` が既にあり、二重管理になるうえ **§7.3 の「変化がなければ
  state を書かない」を壊す**（毎回書き込みが発生してしまう）ため。
- サイズ上限 **512KiB**。超えた場合は `blocks[].h`（表示用 HTML）を捨てて
  `k` のみ保存する。差分検知は引き続き動き、**削除ブロックの中身だけが
  表示できなくなる**（「N ブロック削除」とだけ出す）。

### 6.4 なぜ専用ストレージが要るのか（代替案の検討）

| 案 | 判定 | 理由 |
|---|---|---|
| **前回のブロック列を state に持つ（採用）** | ◎ | 差分表示に必要な粒度で、再パース不要。§4.6 の早期ハッシュ判定もそのまま乗る |
| ハッシュだけ持つ（`content_hash` のみ） | ✕ | 「変わった」ことしか分からず、**どこが変わったかを本文に出せない**。方式 A の価値はそこにあるので不可 |
| 前回の生 HTML を保持する | △ | サイズが 3〜10 倍。毎回パース・除去・正規化をやり直す必要があり、除去規則を変えると過去分の解釈も変わる |
| 前回生成したエントリー本文から復元する | ✕ | `entries` は GC で 30 日で消える（DESIGN.md「GC / リテンション」）。**保持期間の異なるデータに依存してはいけない** |
| ファイルシステムに置く | ✕ | DB 1 ファイルで完結する運用（`VACUUM INTO` バックアップ）を壊す |

`entries` に相乗りできない理由が本質的に重要。エントリーはユーザーが読んだら
消えてよいデータだが、**state は購読が続く限り消してはいけないデータ**で、
ライフサイクルが違う。だから別テーブルに置く。

### 6.5 サイズと書き込み量の見積もり

典型的なお知らせページ（ブロック 200 個、1 ブロックあたりテキスト 40 文字前後）:

| 項目 | 見積り |
|---|---|
| 1 ブロック | `k` 約 120B + `h` 約 180B + JSON オーバーヘッド ≒ 320B |
| 1 ソースの state | 200 ブロック × 320B ≒ **64KB** |
| 監視ソース 50 件 | ≒ 3MB（最悪ケースでも 512KiB × 50 = 25MB） |

DB 全体の見積り（DESIGN.md のリソース見積りで数百 MB）に対して無視できる。
**GC の対象にはしない**（`internal/maintenance` のリテンション処理は
`entries` のみを見る）。

書き込み量は「**変化がなければ state を書かない**」（§7.3）ことで抑える。
50 ソースが週 1 回更新される想定なら週 3MB 程度の書き込みで、
WAL やバックアップへの影響はない。逆にこれをやらないと
50 ソース × 24 回/日 × 64KB ＝ **日 76MB** の無駄な書き込みになるため、
この最適化は必須の扱いとする。

### 6.6 state の無効化と再同期

**保存済み state は、比較の前提（除去規則と `ignore_patterns`）が変わると
そのまま使えない。** 旧前提でマスクされた `k` と新前提の `k` を比較すると、
ページが変わっていなくても全ブロックが差分として湧く。

無効化のトリガは 3 つ:

1. ユーザーが `ignore_patterns` 等を変更した（`config_hash` 不一致）
2. feedla 側の除去規則を更新した（`rules_version` 不一致）
3. state の JSON が壊れている / `version` が想定外

対応:

- **1・2 のうち `blocks[].h` が残っている場合は、保存済みの表示用 HTML から
  `k` を再計算する**（新しい `ignore_patterns` でマスクし直すだけ）。
  取得もエントリー生成も発生せず、ユーザーから見て何も起きない。これが基本経路。
- `h` を捨てている（`truncated` 経路）か、3 の場合は **再同期モード**に入る:
  次回のクロールで取得したブロック列を state として保存するだけで、
  **エントリーは生成しない**。
- 再同期モードを設けないと、設定を 1 行変えるたびに「ページ全文」エントリーが
  1 件湧く。誤検知抑制のために `ignore_patterns` をいじるのに、
  いじると通知が来るのでは本末転倒になる。

初回登録時（state が NULL）だけは例外で、再同期モードには入らず
「監視を開始しました」の全文エントリーを 1 件作る（購読直後に記事ペインが
空だと、動いているのか分からないため）。

### 6.7 OPML・バックアップとの関係

- **OPML export**: `feed_url LIKE 'pagewatch:%'` の購読は OPML から除外する。
  OPML の `xmlUrl` は「フィードの URL」であり、他のリーダーが読んだときに
  壊れた購読になるため。
- **OPML import**: `xmlUrl` が `pagewatch:` で始まる outline は無視する。
- 代わりに `GET /api/v1/scrape_sources` が全設定を JSON で返し、
  `POST` で復元できる（§8.1）。DESIGN.md のバックアップ戦略
  （「最悪 DB を捨てて OPML から再構築」）は、方式 A の設定については
  **この JSON も併せて保存する必要がある**ことを DESIGN.md 側にも追記する。
- **state はバックアップに含めなくてよい**。`VACUUM INTO` の日次バックアップには
  自然に含まれるが、JSON エクスポートには `config` だけを出して `state` は
  出さない。失われても次回クロールで再構築でき（§6.6 の再同期モード）、
  復元してもズレた差分を生むだけの一時データだから。
  **設定は残すべきデータ、state は捨ててよいデータ**という線引きを保つ。

## 7. crawler への統合

### 7.1 分岐点

`crawlOne` に 1 箇所だけ分岐を足す。fetch 前に方式を判定し、fetch 後の
「パース → エントリー化」だけを差し替える。

```go
// crawlOne の冒頭
target, isScrape := strings.CutPrefix(f.FeedURL, "pagewatch:")
if !isScrape {
    target = f.FeedURL
}
// fetch は共通（Accept だけ変える）
fr, err := c.fetcher.Fetch(ctx, target, crawler.FetchOptions{
    ETag: etag, LastModified: lastModified, Accept: acceptFor(isScrape),
})
...
var parsed *ParsedFeed
if isScrape {
    parsed, err = c.extractPage(ctx, f, target, fr, now)   // ← 新規
} else {
    parsed, err = ParseFeed(f.FeedURL, fr.Body, now)       // ← 既存のまま
}
```

- `Fetcher.Fetch` のシグネチャを `FetchOptions` 構造体に変更する
  （現在の `(ctx, url, etag, lastModified)`）。呼び出し元は
  `crawler.crawlOne` と `feed.DiscoverFeed` の 2 箇所のみ。
- `Accept` は方式 A では
  `text/html,application/xhtml+xml;q=0.9,*/*;q=0.8` を送る。
  現在の feed 向け Accept のままだと、サーバによっては XML 表現を返してくる。
- 304 / 410 / 4xx / 5xx / リダイレクトの扱いは **既存コードのまま共通**。
  適応間隔（`nextIntervalOnSuccess`）も新規エントリー数で駆動されるので流用できる。

### 7.2 charset

フィード取得は `gofeed`（内部の `goxpp`）が charset を吸収していたが、
HTML では feedla 自身が判定する必要がある。

```go
r, err := charset.NewReader(bytes.NewReader(fr.Body), fr.ContentType)
```

`golang.org/x/net/html/charset` は `Content-Type` ヘッダ →
`<meta charset>` / `<meta http-equiv>` → BOM → 内容推定の順で判定する。
Shift_JIS / EUC-JP の日本語サイトが対象に入りうるので必須。
デコード失敗時は「そのまま UTF-8 として扱う」ではなく **external エラー**にする
（文字化けした本文で差分が湧くのを防ぐ）。

### 7.3 state 更新の順序と耐障害性

1. `UpsertEntries` でエントリーを書く
2. 成功したら `scrape_sources.state` を更新する
3. `UpdateFeedAfterFetch` で interval / status を更新する（既存）

**差分がなければ 2 をスキップする**（`content_hash` が前回と一致するケース）。
state の中身が変わらないのに毎回 64KB を書き直すと、無変化のページを
1 時間ごとに叩くだけで日 76MB の書き込みになる（§6.5）。
`Extract` が `Result.State = nil` を返したら「更新不要」と解釈する規約にして、
crawler 側に方式固有の判断を持ち込まない。

副作用として、`ignore_patterns` でマスクされた部分**だけ**が変わった場合、
state に残る `blocks[].h`（表示用 HTML）は古いままになる。差分検知の
入力は `k` なので判定には影響せず、次に本物の差分が出たときに
まとめて更新される。削除ブロックの表示に一世代古い文言が出うる点は許容する。

2 が失敗しても、次回クロール時に同じ差分が再検出されるだけで、
**GUID が内容ベースなので重複エントリーにはならない**（`UpsertEntries` の
UPDATE 経路に入る）。逆順（state を先に更新）だと、エントリー書き込みに
失敗した差分が永久に失われる。

state の書き込み失敗・エントリー書き込み失敗はいずれも
`FeedResult.Internal = true`（feedla 側の問題）として扱い、
`feeds.error_count` を汚さない — 既存の内部エラー分類に従う。

### 7.4 エラー分類

| 事象 | 分類 | `error_count` | 備考 |
|---|---|---|---|
| HTTP エラー / タイムアウト | external | ++ | 既存と同じ |
| charset デコード失敗 | external | ++ | |
| HTML パース失敗 | external | ++ | `x/net/html` はほぼ失敗しないが一応 |
| 除去後にブロックが 0 件 | external | ++ | 構造変更 or bot ブロックの疑い。§11 |
| `ignore_patterns` のコンパイル失敗 | external | ++ | 本来は API で弾いているはず |
| store 書き込み失敗 | internal | 据置 | 既存の方針どおり |

「ブロック 0 件」を external エラーにするのは、**403 の代わりに空の
チャレンジページを返す bot 対策**や、サイトの全面リニューアルを
`error_count` 経由で UI に浮かび上がらせるため。

## 8. API

### 8.1 監視ソースの CRUD

```
POST   /api/v1/scrape_sources          {kind, url, folder_id?, title?, config?}
GET    /api/v1/scrape_sources          一覧（バックアップ用途も兼ねる）
GET    /api/v1/scrape_sources/{id}
PATCH  /api/v1/scrape_sources/{id}     {config}（ignore_patterns の編集）
DELETE /api/v1/scrape_sources/{id}     ※ 購読解除は既存の DELETE /subscriptions/{id}
POST   /api/v1/scrape_sources/{id}/preview   今すぐ取得し、抽出結果を返す（保存しない）
```

- `POST` は内部で
  `UpsertFeed("pagewatch:"+url, ...)` → `UpsertSubscription` →
  `scrape_sources` INSERT → `crawler.CrawlFeed`（初回クロール）を行い、
  既存の `handleCreateSubscription` と同じ形の `SubscriptionView` を返す。
- `preview` は `ignore_patterns` を調整するための導線。
  抽出後のブロック列（テキストと、無視パターンでマスクされたか）を返し、
  UI で「このパターンで何が消えるか」を確認できるようにする。
  **保存も差分判定もしない**ので副作用がない。
- `config` の正規表現は受け取り時に `regexp.Compile` して 400 で弾く。
  パターン数は 50、1 本あたり 1000 文字を上限とする（ReDoS の緩和として
  `regexp`（RE2）はバックトラックしないので指数爆発はしないが、
  巨大パターン × 5000 ブロックの CPU 時間は抑えたい）。

### 8.2 既存 API への影響

- `SubscriptionView` に `kind` フィールド（`"feed"` | `"pagewatch"`）を追加し、
  `feed_url` は **接頭辞を外した実 URL** を返す。
  SQL 側で `feed_url` の接頭辞を見て導出するので JOIN は不要。
- `POST /api/v1/subscriptions` は変更しない。フィードが見つからない場合は
  従来どおり 502 を返し、**UI がその場で「ページ監視として登録する」導線を出す**
  （§9.1）。discover の意味論を「見つからなければ監視に切り替える」と
  暗黙に変えると、単なるタイポの URL まで監視登録されてしまう。

## 9. Web UI

### 9.1 登録導線

`AddSubscriptionDialog` で購読追加が失敗したとき、エラー文の下に

> このページにフィードが見つかりませんでした。
> [ページの更新を監視する] （フィードの代わりに、ページの変化を記事として通知します）

というボタンを出し、`POST /api/v1/scrape_sources` を叩く。
押下時に「サイト運営者はフィード配信を意図していません。取得間隔は
1 時間に 1 回です」という一文を添える（§10.3 の礼儀に関する注意喚起）。

### 9.2 一覧での区別

`SubscriptionTree` で `kind === 'pagewatch'` の購読にアイコン（👁）を付ける。
未読数・グループ・rating・pin など既存機能はすべてそのまま使える
（entries テーブルを共有しているため）。

### 9.3 監視設定

`FeedDetailOverlay` に「監視設定」セクションを追加:

- 監視対象 URL（読み取り専用）
- 監視モード（§4.7）のトグル:
  「新しく増えた内容だけ通知」（既定） / 「消えた内容も通知」
- 無視パターンの一覧・追加・削除
- 「いま取得して確認」ボタン → `preview` API を叩き、抽出されたブロックを
  一覧表示。無視パターンでマスクされたブロックはグレーアウトして見せる。
- 直近の抽出結果メタ情報（ブロック数、`truncated` フラグ、最終検知時刻）

### 9.4 誤検知からの回復導線（この機能の肝）

エントリー本文の差分表示で、追加/削除ブロックそれぞれに
「このブロックを無視する」ボタンを出し、押すとそのブロックのテキストから
生成した正規表現（記号をエスケープし、数字列を `\d+` に置換したもの）を
`ignore_patterns` に追加する。

方針検討ドキュメントの結論「実装難易度は配線コストより誤検知抑制コストで決まる」に
対する、UI 側からの回答がこれ。ユーザーは正規表現を書かずに
「これはノイズだ」と教えられる。

## 10. 設定・上限・礼儀

### 10.1 デフォルト値

| 項目 | 値 | 備考 |
|---|---|---|
| `watch_mode` | `"additions"` | §4.7。削除は通知しない |
| `include_full_body` | true | §5.2 の全文セクション |
| `guid_mode` | `"content"` | §5.3 |
| 初期 `fetch_interval_sec` | 3600（1 時間） | 通常フィードの 1800 より緩い |
| 最小間隔 | 既存の `MIN = 10min` | 適応制御で縮まっても下限は共通 |
| 最大間隔 | 既存の `MAX = 12h` | |
| レスポンス上限 | 既存の 10 MiB | |
| ブロック数上限 | 5000 | 超過は切り捨て + UI 警告 |
| state サイズ上限 | 512 KiB | 超過時は表示用 HTML を捨てる |
| 無視パターン数 | 50 | |

### 10.2 既存の礼儀機構の流用

`HostSemaphore`（同一ホスト同時 2・最低 1 秒間隔）、SSRF 対策 dialer、
リダイレクト 5 ホップ制限、条件付き GET はすべて `Fetcher` 側にあるので
そのまま効く。**方式 A で追加の礼儀機構は要らない**（1 ソース = 1 リクエスト）。

なお `feeds.body_hash` は現在の実装では**書き込まれているだけで比較には
使われていない**（`crawler.go:333` で書き、読む箇所がない）。方式 A でも
生 HTML のハッシュは使わず、§4.6 の正規化後ハッシュのみで判定する
（生 HTML はカウンタや CSRF トークンで毎回変わるため、早期スキップの
役に立たない）。

### 10.3 robots.txt を F0 で実装しない理由

- 対象は**ユーザーが明示的に 1 件ずつ登録した URL** であり、クローラによる
  URL の自動発見・追跡がない。
- 1 URL につき 1 時間に 1 回、条件付き GET 付き。人間がブラウザで
  リロードするより明確に軽い。
- robots.txt の取得自体が追加のリクエストとキャッシュ管理を要求し、
  F0 のスコープ（誤検知抑制に時間を割く）を圧迫する。

Phase F1（一覧ページ + N 件の個別ページ取得）では取得件数が桁で増えるため、
**そこで robots.txt 対応を必須要件として再検討する**。この判断は
方針検討ドキュメントの未解決論点に残っているものを、F0 に限って
「実装しない」と決めたもの。

## 11. テスト計画

| 層 | 内容 |
|---|---|
| `extract/pagewatch` 単体 | golden HTML → 期待ブロック列。DB も HTTP も使わない。<br>・ヘッダ/フッタ/nav が落ちること<br>・`class="ad-banner"` が落ち、`class="sub-headers"` が残ること<br>・カード内 `<header>`（深い位置）が残ること<br>・Shift_JIS の HTML が読めること<br>・トラッキングパラメータ付き `href` の揺れで差分が出ないこと |
| 差分単体 | 前回/今回のブロック列ペア → 期待 Added/Removed。順序入れ替えのみ = 差分なし。2000 ブロック超で集合差分にフォールバックすること |
| サニタイズ整合 | 生成した差分 HTML を `bodyPolicy.Sanitize` に通しても `<ins>`/`<del>` が残ること |
| crawler 統合 | `httptest.Server` が 1 回目と 2 回目で異なる HTML を返す → エントリー 2 件（初回 + 差分 1 件）。同じ HTML なら 1 件のまま。時刻表示だけ変わるページ + `ignore_patterns` で 1 件のまま |
| state | ・差分なしのクロールで `state` の `updated_at` が動かないこと（§7.3）<br>・`ignore_patterns` 変更後、保存済み `h` から `k` を再計算して差分が湧かないこと<br>・`version`/`rules_version` 不一致で再同期モードに入り、**エントリーが 0 件**であること<br>・壊れた JSON が入っていてもパニックせず再同期に落ちること |
| store | 一時ファイル DB で `scrape_sources` の CRUD と CASCADE 削除 |
| API | `POST /scrape_sources` → `GET /subscriptions` に `kind: "pagewatch"` で出る。不正な正規表現が 400 |
| MVP ゴールデン | §14.3。実在 2 サイトの過去版→現在版を `testdata/` に置き、期待エントリー数を固定する。**ネットワークアクセスはせず、保存済み HTML を食わせる** |
| e2e | 監視ソースを 1 件登録 → 記事が出る → **末尾で必ず購読解除**（e2e スイートは DB とサイドバーを共有しており、残すと後続 spec の順序を壊す） |

## 12. 作業分解

| # | 作業 | 依存 | 完了条件 |
|---|---|---|---|
| 1 | `internal/extract` 共通型 + `pagewatch` 抽出・差分・state の入出力と再同期判定（§6.6） | — | 単体テストが緑（DB 不要） |
| 2 | `0004_scrape_sources.sql` + store CRUD（`state` は独立して UPDATE できること） | — | store テスト緑 |
| 3 | `Fetcher.Fetch` を `FetchOptions` 化 + charset デコード | — | 既存テスト緑のまま |
| 4 | `crawlOne` の分岐 + state 更新 | 1,2,3 | crawler 統合テスト緑 |
| 5 | API（CRUD / preview）+ `SubscriptionView.kind` | 2,4 | API テスト緑 |
| 6 | Web UI（登録導線・アイコン・監視設定・無視ボタン） | 5 | 手動確認 + e2e |
| 7 | OPML export/import の除外、DESIGN.md 追記 | 5 | round-trip テスト緑 |

**Phase F0 の受け入れ条件**: §14.3 のとおり、実在の 2 サイト
（`www.a-k-r.org/d/`、`tabesugi.net/memo/cur/cur.html`）を購読して
実用になること。判定はゴールデンテストを主、実運用観察を従とする。

## 13. 既知の制約・持ち越す論点

- **フラッピングは 1 回目しか通知されない**（§5.3 の内容ベース GUID）。
  障害 → 復旧 → 障害を個別に読みたい要望が出たら `guid_mode: "revision"` を実装する。
- **落としすぎの救済手段がない**。ハードコード除去規則が本文を巻き込んだ場合、
  F0 の UI からは戻せない。`Config.scope_selector`（CSS セレクタで監視範囲を
  明示指定）を将来足す枠として Config に予約しておくが、実装には
  `cascadia` 等の依存追加が必要になるため F0 では見送る。
- **bot 対策サイトは対応できない**。403 や空のチャレンジページは
  「ブロック 0 件」エラーとして UI に出るところまでが F0 の責任範囲で、
  回避策は講じない。
- **著作権・利用規約**: サイト運営者は全文配信を意図していない。
  登録時の注意喚起（§9.1）に留め、それ以上の制限は設けない。
- **DESIGN.md 未決事項 #1（フィード内の全文取得）との共有**は、
  §4.1 のとおり HTTP 取得・charset・サニタイズ層に限られる。
- **Phase F1 への申し送り**: `Extractor` インターフェースと不透明 `State` が
  `urlpattern` 方式でも無理なく使えるかを、F0 の実装時に
  「一覧ページから `<a>` を集めるだけの実験実装」で軽く検証しておく。
  ここで境界が破綻するなら F1 の前に直す。

## 14. MVP — 実在の 2 サイトで検証する

**F0 の完了判定は、次の 2 ページを購読して実用になることとする。**
どちらも `<link rel="alternate">` を持たず、`index.rdf` / `atom.xml` 等の
慣習的なパスも 404 で、**フィードを一切配信していない**ことを確認済み。

| | 天泣記 | タベスギ・メモ |
|---|---|---|
| URL | `http://www.a-k-r.org/d/` | `https://tabesugi.net/memo/cur/cur.html` |
| サイズ | 29 KB | 2.3 KB |
| `ETag` / `Last-Modified` | あり | あり |
| charset の在処 | **Content-Type ヘッダのみ**（`<meta charset>` なし） | **`<meta http-equiv>` のみ**（ヘッダに charset なし） |
| DOCTYPE | HTML 4.01 Strict | HTML5 |
| `script` / 広告 | なし | なし |
| 記事の区切り | `<h2>` 日付 + `<dt class="content">` 見出し + `<dd>` 本文 | `<h2 class="headline">` + 後続の `<p>` 群 |
| 記事ごとのアンカー | `id="a2026_07_30_5"` | `id="202608120735"` |
| 掲載形式 | 最新 5 日分のローリング | **当月分のみ**（月替わりで全入れ替え） |

**この 2 つが揃って示すこと**: 個人サイトの日記は
「フィードがない・HTML は素直・記事アンカーはある・条件付き GET は効く」
という共通の性質を持ち、方式 A の主戦場は当初想定していた
ステータスページ（対象サイト例パターン 5）よりむしろこちらである。

### 14.1 天泣記から得られた設計変更

- **ローリング表示なので、新記事が入ると古い記事が押し出される。**
  差分をそのまま出すと `<del>`（消えた記事の全文）がエントリーの大半を占める。
  → `watch_mode: "additions"` を導入し、**既定にした**（§4.7）。
- 末尾に月別アーカイブの `<ul>` があり、月が変わると
  `<li>2026: 01, 03, 06, 07</li>` → `…, 07, 08</li>` と 1 ブロックだけ変わる。
  → `ignore_patterns` の実例として、登録時のプリセット候補にする:
  `^\d{4}: (\d{2}(, )?)+$`
- `class` は `content` のみ、`id` は `a2026_07_30_5` 形式。
  §4.2 の除去語（語の完全一致）には**何も一致しない** = 落としすぎが起きない。
  ここが「部分一致にしなくてよかった」ことの実証になる
  （`a2026_07_30_5` を `[-_]` で割ると `a2026`/`07`/`30`/`5`）。

### 14.2 タベスギ・メモから得られた設計変更

- `<div class="lastmod">Last Modified: Tue Aug 11 22:45:43 UTC 2026</div>` と
  `<div class="docid">Document ID: 787075d3…</div>` を持つ。
  **本文が 1 文字も変わらなくても、再生成しただけで差分が出る**。
  → §4.2 の除去語に `lastmod` / `lastmodified` / `docid` を追加した。
  これを入れないと MVP の時点で誤検知が常態化する。
- `<div class="nav">` が上下に 2 つ（`^ up` / `< Prev` / `^ Top`）。
  → 除去語 `nav` の完全一致でどちらも落ちる。設計どおり。
- **`Content-Type: text/html` に charset がなく、`<meta http-equiv>` にだけある。**
  → §7.2 で `x/net/html/charset` を通す設計が必須であることの実証。
  ヘッダだけを見る実装だと日本語が化け、化けた本文で差分が湧く。
- `<p>` を閉じない古い書き方（`<p>` の直後に `<div>` が来る）。
  → HTML5 パーサ（`x/net/html`）が暗黙終了を正しく処理するので、
  §4.4 のブロック分割はそのまま機能する。自前でタグを数える実装なら壊れていた。
- 当月分のみ掲載なので、**月替わりでページ全体が入れ替わる**。
  `watch_mode: "changes"` なら月初に「全記事削除」の巨大エントリーが立つ。
  → ここでも `additions` 既定が正しいことが裏付けられる。

### 14.3 MVP の受け入れ条件

実際の更新は月 1 回程度（天泣記の直近は 2026-01 / 03 / 06 / 07）なので、
「1 週間動かして観察」では取りこぼしを検証できない。**過去版を使った
ゴールデンテスト**を主、実運用観察を従とする。

1. **ゴールデンテスト**（必須）: 各サイトについて、
   Internet Archive から取得した過去版と現在版をテストデータとして
   `testdata/` に置き、順に食わせて
   - 1 回目: state を作り、全文エントリー 1 件
   - 2 回目: **新しく増えた記事だけ**が本文に出るエントリー 1 件
   - 3 回目（同じ HTML を再投入）: エントリー 0 件
   となること。天泣記は 2025-12 版のスナップショットが取得可能なことを確認済み。
2. **ノイズ耐性**（必須）: タベスギの `lastmod` / `docid` だけを書き換えた
   HTML を投入して **エントリーが 0 件**であること。
3. **実運用観察**（従）: 2 サイトを 2 週間監視し、誤検知が 0 件であること。
   更新が来れば取りこぼしも確認するが、来ない可能性が高い。

### 14.4 MVP で割り切ること（F0.5 への申し送り）

**1 回の更新で複数記事が増えると、1 つのエントリーにまとまる。**
天泣記は 1 日に `#1`〜`#5` と複数書くため、実際に起きる。
未読 5 件ではなく未読 1 件になり、その本文に 5 記事分が縦に入る。

MVP としてはこれで「購読できる」が、LDR 的な読み流し体験としては物足りない。
両サイトとも記事ごとのアンカー（`a2026_07_30_5` / `202608120735`）と
見出しを持っているので、**追加ブロック列を見出し境界で切り、
記事ごとのエントリーにする拡張**（F0.5）が自然に載る:

- GUID = アンカーを含む URL（`…/d/#a2026_07_30_5`）
- タイトル = 見出しのテキスト
- 本文 = 次の見出しまでのブロック群

そのために §4.4 の `Block.Anchor` / `Block.Head` を **F0 の時点から
state に保存しておく**（後付けだと全 state が再同期になる）。
F0.5 を F0 に含めない理由は、誤検知抑制（本題）の検証と
記事分割の検証を同時に走らせると、問題の切り分けができなくなるため。
