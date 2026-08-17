# CLAUDE.md

feedla の開発運用に関する指示。設計・アーキテクチャは docs/DESIGN.md を参照。

## ツールチェーン

- go/lefthook/golangci-lint/pnpm は mise で管理する。lefthook 経由で mise 管理外のツールを呼ぶ場合は
  `mise exec pnpm -- pnpm ...` のように `mise exec` 越しに呼び、go の PATH には触れないこと。
- pre-commit で lefthook + golangci-lint + web-typecheck が走る。
- `go build` の前に `pnpm run build`(または `make build`)でフロントエンドをビルドしておくこと
  (`internal/web/dist` は `.gitignore` 対象で go:embed の対象)。

## セキュリティ

- **認可判定を伴うエンドポイントの新規追加・変更には、複数ユーザー視点の
  テスト(IDOR テスト)を必須とする。** 所有者以外の第三者ユーザー(必要なら
  admin ユーザーも)でアクセスし、意図どおり拒否される/意図どおり許可される
  ことを検証すること。`internal/api/idor_test.go` の `createTestUser` ヘルパー
  と、`e2e/tests/multi-user-isolation.spec.ts` の2ブラウザセッションパターンを
  再利用する。
  - 理由: マルチユーザー化 Phase C で見つかった認可漏れ4件(`docs/security-review-2026-08.md`
    「追記(2026-08-17)」節)は、すべて「所有権/購読チェックの書き忘れ」で
    混入しており、単一ユーザー視点の単体テストでは検出できなかった。うち
    `AddPin` の実装バグ(未購読フィードへの pin が可能だった)は、事前の設計
    レビューでも見逃され、クロスユーザーテストを書いて初めて発見された。
  - 対象: リソース ID をパスパラメータ/ボディで受け取り、DB から取得して
    返す・更新する・削除する・副作用のある操作(クロール強制実行や外部 URL
    fetch を伴う preview 系を含む)を行う全エンドポイント。

## Git ワークフロー

- **main に直接コミットしない。** 作業は `feat/xxx`・`fix/xxx` のようなトピックブランチで行い、
  `gh pr create` で PR を作成し、`gh pr merge --auto --merge` で auto-merge を有効化する
  (CI が通り次第、ユーザーの追加確認なしに自動でマージされる)。
- tagpr のリリース PR (`tagpr-from-*`) も同じ auto-merge 運用に乗る。
- 作業開始前に `git fetch && git log HEAD..origin/main` で最新化を確認し、必要なら
  `git pull --ff-only` してからブランチを切ること。
- force push や履歴改変など明確に破壊的な git 操作は対象外(通常どおり別途確認する)。
