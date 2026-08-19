# ADR 0001: フィード本文に含まれる第三者SNS投稿embedの扱い

ステータス: **実装済み（案B改: クライアント側iframe置換、既定オフのオプトイン）**
日付: 2026-08-19
レビュー: 別視点モデルによる評価済み（本文中の指摘反映）

## コンテキスト

一部のブログサービスは、SNS の投稿を記事に貼り付けると、記事本文
(RSS の `content:encoded` / `description`)に次のような HTML を
そのまま出力する(構造はプロバイダにより多少異なるが、概ね共通のパターン)。

```html
<blockquote class="sns-embed"
    data-embed-permalink="https://sns-provider.example/p/XXXXXXXXXXX/">
  ...
</blockquote>
<script async src="//sns-provider.example/embed.js"></script>
```

feedla の現行実装では、これは意図通りブロックされる。

- 本文サニタイズ: `internal/crawler/parser.go:26` の
  `bodyPolicy = bluemonday.UGCPolicy()` が `<script>` を含め
  ホワイトリスト外の要素をすべて除去する
  (`normalizeItem()` 内で `bodyPolicy.Sanitize(rawBody)`)。
- CSP: `internal/web/web.go:15` の
  `script-src 'self'; frame-src 'none'` により、仮にサニタイズを
  すり抜けても外部 script の実行・外部 iframe の埋め込みはブラウザ側で
  ブロックされる(`docs/DESIGN.md` §セキュリティ（表示面）参照)。

結果として、そうした記事は「元の投稿へのリンク」テキストが残るだけの
見た目になる。この体験を改善する余地があるかを検討した。

## 検討した選択肢

### 案 A: script タグを URL パターン限定で許可する

bluemonday はタグ単位だけでなく `AllowAttrs("src").Matching(regexp).OnElements("script")`
のように属性値を正規表現で絞った許可ルールを組める。
`src` が特定プロバイダの embed script URL に厳密一致する `<script>` のみ
通す、という実装は可能。

- 長所: プロバイダ公式の embed script をそのまま使うため見た目・機能の
  再現度が高い。
- 短所: 「投稿者由来の script は常に無効化する」という UGCPolicy の
  根幹の前提を崩す。ドメインを絞っても、embed script の中身は feedla の
  管理外であり、将来の改変や配信経路の侵害があった場合に feedla の
  ページコンテキストで任意コードが実行される。CSP の `script-src` も
  `'self'` から緩めが必要になり、影響範囲が広い。

### 案 B: iframe に差し替える(oEmbed 相当)

SNS プロバイダの多くは投稿単体の iframe 埋め込み用 URL を提供している。
本文中の embed permalink から投稿 ID を抽出し、サニタイズ後の本文で
blockquote を `<iframe src="..." sandbox="allow-scripts allow-same-origin allow-popups">`
に置換する。

- 長所: プロバイダ由来の JS は iframe 内(別オリジン)で実行され、
  ブラウザの同一オリジンポリシーにより feedla の DOM・Cookie には
  到達できない。CSP の変更も `frame-src` のみで済み、`script-src 'self'`
  は変更不要。
- 短所: 表示のたびにプロバイダのサーバへリクエストが発生する
  (トラッキング・プライバシー面の考慮が要る)。CSP の `frame-src 'none'`
  を許可プロバイダのドメインへ緩める変更が必要
  (`internal/web/web.go:15`、コード直書き定数のためリビルド必須)。
  プロバイダ側の iframe 仕様変更に追従が必要。
  また embed script(未読み込み)が担っていた高さ自動調整(postMessage
  によるリサイズ)が効かなくなるため、固定高で内容が見切れる/
  余白が生じる可能性があり、案Aに対して再現度で劣る面がある。

### 案 C: サーバー側でスクレイピング/API取得して静的コンテンツに置換

プロバイダ側からキャプション・画像を取得し、feedla 側で自前ホストした
静的コンテンツとして表示する。

- 長所: CSP・実行コンテキストの緩和が一切不要(自ドメイン内で完結)。
- 短所: この種の SNS プロバイダの公式 oEmbed API は多くの場合アプリ登録・
  アクセストークンが必須で、非公式スクレイピングは HTML 構造変更で
  頻繁に壊れる・bot 検知でブロックされる・利用規約違反のリスクがある。
  画像の自前ホスティング/キャッシュも必要になり、実装・運用コストが
  他の2案より大きい。

### 案 D: 静的リンクカードへの整形(embed自体を諦める)

blockquote 内のテキスト・permalink URL のみを抽出し、外部への通信や
JS 実行を一切伴わない静的なリンクカード(サムネイルなし、または
og:image 等を別途取得する場合はそこだけ画像プロキシ経由)として表示する。

- 長所: 通信・実行コンテキストとも feedla の管理下から一切出ない。
  実装が最も単純でCSP変更も不要。
- 短所: 埋め込み本来の見た目(投稿の画像・キャプション)は再現できず、
  「元投稿へのリンク」に留まる点は現状とほぼ変わらない。

## 比較

| | 案A: script許可 | 案B: iframe置換 | 案C: スクレイピング | 案D: 静的リンクカード |
|---|---|---|---|---|
| CSP変更 | `script-src` 緩和(影響大) | `frame-src` 緩和(影響小) | 不要 | 不要 |
| サンドボックス | なし(同一オリジンで実行) | あり(別オリジン) | 該当なし | 該当なし |
| 実装コスト | 中 | 中 | 大 | 小 |
| 保守コスト | プロバイダ側変更に追従 | プロバイダ側変更に追従 | 高(構造変更・規約違反リスク) | 低 |
| プライバシー | 表示のたびプロバイダへ通信 | 同左 | feedla側でキャッシュ可能 | 通信なし |
| 見た目の再現度 | 高 | 中(高さ自動調整なし) | 高 | 低(リンクのみ) |

## 決定

**案 B(iframe置換)を採用したが、変換を行う場所とオプトインの要否を
当初案から変更した。**

検討時点では「クロール時にサーバー側で blockquote を iframe に書き換え、
DB に保存する」形を軸に考えていたが、実装前に以下の理由でクライアント側
(表示時)変換・既定オフのオプトインに変更した。

- **entries は feed 単位で1行しか持たない**(`internal/store/migrations/0001_init.sql`)。
  同じ feed を複数ユーザーが購読していても本文は共有されており、クロール時に
  DB へ焼き込む方式では「ユーザーごとに ON/OFF を選べる」設定にできない
  (焼き込んだ瞬間、その feed を購読する全員に一律適用されてしまう)。
- feedla にはまだユーザー単位の汎用設定機構(表示設定を保存するテーブル/API)
  が存在しない。新設するのは今回のスコープに対して重い一方、
  ブラウザの localStorage に閉じたクライアント側トグルであれば追加の
  バックエンド実装なしで「既定オフのオプトイン」を素直に実現できる。
- クライアント側変換にすると、当初懸念していた Consequences
  (既存エントリが再クロールされるまで対象にならない/ロールバック時に
  DB に iframe マークアップが残る/`entries.body` が常に `UGCPolicy` の
  出力そのものという不変条件が崩れる)がすべて解消される。
  `entries.body` は今まで通り `bodyPolicy.Sanitize()` の出力そのままで、
  DB には一切 iframe を書き込まない。

**最終実装:**

- サーバー側(`internal/crawler/parser.go`)は `bodyPolicy` に
  `data-instgrm-permalink` 属性を、Instagram の投稿/リール permalink の
  形に厳密一致する場合のみ `blockquote` 要素上で許可する例外を追加した
  だけで、iframe への書き換えは一切行わない。`class="instagram-media"`
  も同様に値をこの1つに限定して許可(UGCPolicy は素の状態では `class`
  属性自体を許可していないため)。permalink の形式チェックは
  `^https://(?:www\.)?instagram\.com/(?:p|reel)/[A-Za-z0-9_-]+/(?:\?...)?$`
  で、パストラバーサルやホスト詐称(`instagram.com.evil.example` 等)を
  防ぐ。`<script>` は従来通り常に除去される。
- クライアント側(`web/src/state/settings.ts`)に
  `instagramEmbedsEnabled`(localStorage永続化、**既定 false**)を追加し、
  サイドバーメニューにトグルを置いた。ON のユーザーのみ、
  `web/src/utils/instagramEmbed.ts`(Go側と同じ形式チェックを
  TypeScript で再実装したもの)が `EntryItem` の描画後に
  `blockquote.instagram-media[data-instgrm-permalink]` を検出し、
  サンドボックス化した `<iframe>`(`sandbox="allow-scripts allow-popups"`
  ―`allow-same-origin` なし、`referrerpolicy="no-referrer"`、
  `loading="lazy"`)に置き換える。
- CSP(`internal/web/web.go`)は `frame-src 'none'` から
  `frame-src https://www.instagram.com` に変更。誰が iframe を生成する
  (サーバー/クライアント)かに関わらずブラウザは同じ CSP を見るため、
  この変更はクライアント側方式でも変わらず必要。

案A(script許可)は自ドメインでの実行コンテキストになる隔離性の低さ、
案C(スクレイピング)は保守コスト・規約リスクの観点で見合わないという
当初の判断は変わらない。案D(静的リンクカード)はオプトインかつ
クライアント側変換にしたことで実装コストの差が縮まったため優先度は
下がったが、今後 iframe 方式で問題が出た場合の代替として残しておく。

## 影響・フォローアップ

- 対象記事のプレビュー表示(pagewatch由来のdiffプレビュー等)でも
  同じ変換を通すか要検討(現状はエントリー本文の描画パスのみ対応)。
- 動作確認: 実際のブログ記事(Instagram投稿埋め込みを含むフィード)を
  feedla 本体で購読・クロールし、ブラウザ(Playwright)で
  「既定オフでは iframe が生成されないこと」「トグル ON で実際の投稿が
  レンダリングされること」「`<script>` の残留やfeedla側の実CSP違反が
  ないこと」を確認済み。
