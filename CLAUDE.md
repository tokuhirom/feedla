# CLAUDE.md

feedla の開発運用に関する指示。設計・アーキテクチャは docs/DESIGN.md を参照。

## ツールチェーン

- go/lefthook/golangci-lint/pnpm は mise で管理する。lefthook 経由で mise 管理外のツールを呼ぶ場合は
  `mise exec pnpm -- pnpm ...` のように `mise exec` 越しに呼び、go の PATH には触れないこと。
- pre-commit で lefthook + golangci-lint + web-typecheck が走る。
- `go build` の前に `pnpm run build`(または `make build`)でフロントエンドをビルドしておくこと
  (`internal/web/dist` は `.gitignore` 対象で go:embed の対象)。

## Git ワークフロー

- **main に直接コミットしない。** 作業は `feat/xxx`・`fix/xxx` のようなトピックブランチで行い、
  `gh pr create` で PR を作成し、`gh pr merge --auto --merge` で auto-merge を有効化する
  (CI が通り次第、ユーザーの追加確認なしに自動でマージされる)。
- tagpr のリリース PR (`tagpr-from-*`) も同じ auto-merge 運用に乗る。
- 作業開始前に `git fetch && git log HEAD..origin/main` で最新化を確認し、必要なら
  `git pull --ff-only` してからブランチを切ること。
- force push や履歴改変など明確に破壊的な git 操作は対象外(通常どおり別途確認する)。
