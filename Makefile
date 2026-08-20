.PHONY: build web-dev e2e

build:
	cd web && pnpm install && pnpm run build
	go build ./...

web-dev:
	cd web && pnpm install && pnpm run dev

# Runs the Playwright e2e suite against the real feedla API/crawler/SPA
# wiring (see e2e/testserver). Requires a frontend build (`build` target)
# since the server it drives embeds internal/web/dist via go:embed.
e2e: build
	cd e2e && pnpm install && pnpm exec playwright install chromium && pnpm test
