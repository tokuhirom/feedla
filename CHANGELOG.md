# Changelog

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
