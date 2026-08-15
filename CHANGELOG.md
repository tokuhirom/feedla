# Changelog

## [v2026.815.8](https://github.com/tokuhirom/feedla/compare/v2026.815.7...v2026.815.8) - 2026-08-15

- feat: フィード管理画面を追加し、購読フィードを検索できるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/89
- feat: フィード管理/詳細画面に強制再クロールボタンを追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/91

## [v2026.815.7](https://github.com/tokuhirom/feedla/compare/v2026.815.6...v2026.815.7) - 2026-08-15

- fix: エントリヘッダーの戻る/タイトル/未読数を1行に集約 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/87

## [v2026.815.6](https://github.com/tokuhirom/feedla/compare/v2026.815.5...v2026.815.6) - 2026-08-15

- fix: スワイプによるj/k相当のエントリ送り機能を廃止 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/83
- feat: エラーのあるフィード一覧に次回取得予定日時を表示 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/85
- feat: エラーのあるフィード一覧にサイトへのリンクを追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/86

## [v2026.815.5](https://github.com/tokuhirom/feedla/compare/v2026.815.4...v2026.815.5) - 2026-08-15

- feat: フィード詳細からカテゴリ(フォルダ)を移動できるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/81

## [v2026.815.4](https://github.com/tokuhirom/feedla/compare/v2026.815.3...v2026.815.4) - 2026-08-15

- フィード/グループ切り替え時にスクロール不要な記事も既読にする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/76
- 日付のないフィードで未読が一気に大量発生するのを防ぐ by @tokuhirom in https://github.com/tokuhirom/feedla/pull/78
- feat: プライオリティ表示にTodayを追加(過去24時間の未読をrating別にまとめ読み) by @tokuhirom in https://github.com/tokuhirom/feedla/pull/80

## [v2026.815.3](https://github.com/tokuhirom/feedla/compare/v2026.815.2...v2026.815.3) - 2026-08-15

- fix: モバイルの戻るボタン連打で feedla の外に出てしまう不具合を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/70
- feat: 非常に長い記事本文を折りたたみ表示にする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/72
- feat: フィード詳細画面から全て既読にできるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/73
- feat: サイドバーのカテゴリ/プライオリティの開閉状態をlocalStorageに保存する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/74

## [v2026.815.2](https://github.com/tokuhirom/feedla/compare/v2026.815.1...v2026.815.2) - 2026-08-15

- fix: スマホ表示でフォーカス枠がスクロールに追従しないのを修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/62
- docs: READMEを利用者向けに書き換え、内部設計をdocs/DESIGN.mdへ分離 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/64
- feat: エラーのあるフィード一覧に最終エラー日時を表示 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/65
- chore: web に Biome を導入 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/66
- feat: モバイルで左右スワイプによる記事送り/戻りに対応 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/67
- fix: entry一覧の区切り線のコントラストを上げる by @tokuhirom in https://github.com/tokuhirom/feedla/pull/68
- feat: グループ表示のヘッダーに現在の記事のフィード名を表示 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/69

## [v2026.815.1](https://github.com/tokuhirom/feedla/compare/v2026.815.0...v2026.815.1) - 2026-08-15

- fix: ボタンにスタイルが当たっておらず見た目が雑な問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/60

## [v2026.815.0](https://github.com/tokuhirom/feedla/compare/v2026.814.14...v2026.815.0) - 2026-08-15

- fix: 未読件数の桁数変化でヘッダーのボタン位置がずれる問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/56
- fix: 未読エントリーが1件かつ縦幅が短い場合に既読にならない問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/58
- fix: 未読エントリーが1件かつ縦幅が短い場合に既読にならない問題を修正 (再修正) by @tokuhirom in https://github.com/tokuhirom/feedla/pull/59

## [v2026.814.14](https://github.com/tokuhirom/feedla/compare/v2026.814.13...v2026.814.14) - 2026-08-14

- fix: モバイル版のフィードタイトルを専用行に分離して折り返しを防止 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/54

## [v2026.814.13](https://github.com/tokuhirom/feedla/compare/v2026.814.12...v2026.814.13) - 2026-08-14

- fix: 既読反映が失敗するSQLite一時ファイルエラーの解消と5xxエラートースト by @tokuhirom in https://github.com/tokuhirom/feedla/pull/52

## [v2026.814.12](https://github.com/tokuhirom/feedla/compare/v2026.814.11...v2026.814.12) - 2026-08-14

- fix: 記事本文の画像がアスペクト比を維持せず縦に間延びする問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/50

## [v2026.814.11](https://github.com/tokuhirom/feedla/compare/v2026.814.10...v2026.814.11) - 2026-08-14

- fix: "エラーのあるフィード" 一覧がスクロールできない問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/44
- "エラーのあるフィード" 一覧にURL表示とフィード詳細へのリンクを追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/46
- fix: 購読追加直後のフィード選択でGETが競合し既読状態が巻き戻ることがある問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/48
- fix: スマホ幅で "エラーのあるフィード" 一覧をポップアップから独立ページ表示に変更 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/49

## [v2026.814.10](https://github.com/tokuhirom/feedla/compare/v2026.814.9...v2026.814.10) - 2026-08-14

- fix: jキーでの次エントリー移動時にタイトルがsticky headerに隠れる問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/34
- feat: サイドバーのフィード表示順を未読優先+最終エントリー日時順に変更 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/40
- fix: サイドバーのフィード切り替え時にメインpaneのスクロール位置をリセット by @tokuhirom in https://github.com/tokuhirom/feedla/pull/41
- fix: j/kをマウスホイールでのスクロール位置に追従させる by @tokuhirom in https://github.com/tokuhirom/feedla/pull/42

## [v2026.814.9](https://github.com/tokuhirom/feedla/compare/v2026.814.8...v2026.814.9) - 2026-08-14

- fix: 既読POSTがリロードで中断され未読に戻ってしまう問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/29
- fix: s/aキーでの次/前フィード選択がサイドバー表示順と一致しない問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/31
- feat: 評価の+/-キーとshift+jでの次フィード遷移を追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/32

## [v2026.814.8](https://github.com/tokuhirom/feedla/compare/v2026.814.7...v2026.814.8) - 2026-08-14

- fix: OPMLインポートでShift_JIS等の非UTF-8宣言があると失敗する問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/26
- fix: APIエラーレスポンスをサーバーログにも出力する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/28

## [v2026.814.7](https://github.com/tokuhirom/feedla/compare/v2026.814.6...v2026.814.7) - 2026-08-14

- fix: 日時表示を固定フォーマット(YYYY-MM-DD HH:mm)にする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/24

## [v2026.814.6](https://github.com/tokuhirom/feedla/compare/v2026.814.5...v2026.814.6) - 2026-08-14

- feat: エントリー日時・フィード名表示とサイドバーfavicon by @tokuhirom in https://github.com/tokuhirom/feedla/pull/22

## [v2026.814.5](https://github.com/tokuhirom/feedla/compare/v2026.814.4...v2026.814.5) - 2026-08-14

- feat: 無視ワードを設定できるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/15
- chore: CLAUDE.mdを追加し、PR+auto-merge運用を明文化する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/16
- feat: サイドバーにカテゴリ/プライオリティ表示切り替えを追加する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/18
- feat: フォルダ/プライオリティグループの記事を一気読みできるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/19
- fix: モバイルUIでのエッジスワイプ戻りがアプリ外に出てしまう問題を修正する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/20
- fix: 極狭幅ビューポートでエラーフィード一覧の購読解除ボタンが潰れる問題を修正する by @tokuhirom in https://github.com/tokuhirom/feedla/pull/21

## [v2026.814.4](https://github.com/tokuhirom/feedla/compare/v2026.814.3...v2026.814.4) - 2026-08-14

- feat: ヘッダーの星評価をクリック/タップで変更できるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/13

## [v2026.814.3](https://github.com/tokuhirom/feedla/compare/v2026.814.2...v2026.814.3) - 2026-08-14

- feat: クロール状況をWeb UIから見えるようにする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/11

## [v2026.814.2](https://github.com/tokuhirom/feedla/compare/v2026.814.1...v2026.814.2) - 2026-08-14

- fix: 購読解除の前に確認ダイアログを出す by @tokuhirom in https://github.com/tokuhirom/feedla/pull/7
- feat: 購読解除をヘッダーの✕からフィード詳細画面へ移動 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/9
- fix: スマホでリスト最後のエントリが既読にならない問題を修正 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/10

## [v2026.814.1](https://github.com/tokuhirom/feedla/compare/v2026.814.0...v2026.814.1) - 2026-08-14

- feat: 空DBでのfeedla serve起動時にデフォルト購読を自動seedする by @tokuhirom in https://github.com/tokuhirom/feedla/pull/3
- feat: モバイル(狭幅ビューポート)向けUIを追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/5
- docs: READMEにスクリーンショットを追加 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/6

## [v2026.814.0](https://github.com/tokuhirom/feedla/commits/v2026.814.0) - 2026-08-14

- ci: goreleaserでlinux/macバイナリとghcr.ioイメージのリリースを自動化 by @tokuhirom in https://github.com/tokuhirom/feedla/pull/2
