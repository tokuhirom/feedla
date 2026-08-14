.PHONY: build web-dev

build:
	cd web && pnpm install && pnpm run build
	go build ./...

web-dev:
	cd web && pnpm install && pnpm run dev
