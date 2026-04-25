# Build tags required by mattn/go-sqlite3 to enable FTS5 and runtime extension loading (Honker).
GO_TAGS = sqlite_fts5 sqlite_load_extension

.PHONY: test build run extension web go clean

test:
	go test -tags '$(GO_TAGS)' ./...

# `web` runs `npm run build`; the SvelteKit adapter-static config writes directly to internal/web/dist.
web:
	cd web && npm ci && npm run build

go:
	go build -tags '$(GO_TAGS)' -o wire ./cmd/wire

build: web go

run: build
	./wire serve

# extension is added in Task 9.2; placeholder here so the target exists for early dev.
extension:
	./scripts/build-honker-extension.sh

clean:
	rm -rf wire build internal/web/dist/*
