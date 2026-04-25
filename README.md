# Wire

A self-hosted feed reader built for speed, simplicity, and offline reading.
Single-binary deployment, SQLite storage, Svelte SPA front-end. Inspired by
Miniflux but opinionated differently — see [`design.md`](design.md) for the
full spec.

## Status

Phase 0 (foundation). Schema, server skeleton, SPA scaffold, container build.
Feed polling and content extraction land in Phase 1.

## Quick start (development)

```bash
make extension   # one-time: build the Honker SQLite extension (Rust cdylib)
make build       # build SPA + Go binary
./wire serve     # listens on :8080 by default
```

## Quick start (Docker)

```bash
docker build -t wire:foundation .
docker run --rm -v wire-data:/data -p 8080:8080 wire:foundation
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WIRE_DB_PATH` | `./wire.db` | SQLite database file |
| `WIRE_LISTEN` | `:8080` | HTTP listen address |
| `WIRE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `WIRE_LOG_FORMAT` | `json` | `json` \| `text` |
| `WIRE_HONKER_EXTENSION_PATH` | `./build/libhonker_ext` | Honker SQLite extension cdylib path (no `.so`/`.dylib` suffix — SQLite appends it) |

## License

MIT — see [`LICENSE`](LICENSE).
