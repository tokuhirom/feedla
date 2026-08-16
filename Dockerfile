# syntax=docker/dockerfile:1

# --- web: build the Preact/Vite SPA -----------------------------------
# web/vite.config.ts writes its build output directly to
# ../internal/web/dist (go:embed can't reach outside internal/web), so this
# stage's output lands at /src/internal/web/dist for the build stage below
# to pick up.
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack prepare pnpm@11.20.0 --activate && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

# --- build: compile the single Go binary --------------------------------
# CGO_ENABLED=0 works because modernc.org/sqlite is a pure-Go SQLite driver
# (see docs/DESIGN.md's "技術選定" note) -- no cgo/musl toolchain needed.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/feedla ./cmd/feedla
# A named volume is created root-owned; baking an empty, correctly-owned
# /data into the image lets Docker's volume-initialization step (copying
# the mountpoint's image contents into a fresh volume) hand it to
# non-root USER 65532 already writable.
RUN mkdir -p /data && chown 65532:65532 /data

# --- final: single binary + CA certs, nothing else ----------------------
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/feedla /feedla
COPY --from=build --chown=65532:65532 /data /data

# Must stay 0.0.0.0 inside the container: `docker run -p` forwards to the
# container's non-loopback interface, so binding 127.0.0.1 here would make
# the port mapping unreachable. Host-side exposure is controlled by the
# `-p` flag at `docker run` time instead (see README.md quickstart, which
# defaults to `-p 127.0.0.1:8080:8080`).
ENV FR_LISTEN=0.0.0.0:8080
ENV FR_DB_PATH=/data/feedla.db
VOLUME ["/data"]
EXPOSE 8080

# Numeric UID (no /etc/passwd in scratch): run unprivileged. The volume
# mounted at /data must be writable by this UID.
USER 65532:65532

ENTRYPOINT ["/feedla"]
CMD ["serve"]
