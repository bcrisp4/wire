# syntax=docker/dockerfile:1.6

# Stage 1 — build the Honker SQLite extension (Rust cdylib).
# The cdylib name is "honker_ext" per honker-extension/Cargo.toml [lib],
# so the artifact is libhonker_ext.so. We build against musl (matches the
# Alpine runtime) but disable the default crt-static feature: `cdylib` cannot
# be produced when libc is statically linked.
FROM rust:1-alpine AS honker-ext
RUN apk add --no-cache musl-dev pkgconfig sqlite-dev git
ARG HONKER_REF=main
ENV RUSTFLAGS="-C target-feature=-crt-static"
WORKDIR /src
RUN git clone --depth 1 --branch ${HONKER_REF} https://github.com/russellromney/honker.git . \
 && cargo build -p honker-extension --release

# Stage 2 — build the SvelteKit SPA. adapter-static writes to /web/../internal/web/dist.
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ .
RUN mkdir -p /internal/web/dist && npm run build

# Stage 3 — build the Go binary with CGo + FTS5 + load_extension.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache build-base sqlite-dev git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /internal/web/dist ./internal/web/dist
ENV CGO_ENABLED=1
RUN go build \
      -tags 'sqlite_fts5 sqlite_load_extension' \
      -ldflags="-s -w" \
      -o /out/wire ./cmd/wire

# Stage 4 — minimal runtime.
FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs tini && \
    addgroup -S wire && adduser -S wire -G wire && \
    mkdir -p /data /usr/local/lib && chown -R wire:wire /data
WORKDIR /data
COPY --from=build /out/wire /usr/local/bin/wire
COPY --from=honker-ext /src/target/release/libhonker_ext.so /usr/local/lib/libhonker_ext.so
USER wire
ENV WIRE_DB_PATH=/data/wire.db \
    WIRE_LISTEN=:8080 \
    WIRE_HONKER_EXTENSION_PATH=/usr/local/lib/libhonker_ext.so
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/wire"]
CMD ["serve"]
