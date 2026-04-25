# Phase 1c — Deferred work

These items were considered during Phase 1b planning and explicitly deferred. They aren't blocking; capture each one's motivation and exact touchpoints so a future maintainer (human or agent) can pick any of them up without re-deriving the design.

## 1. Cursor-based pagination on `/entries`

**Today**: `GET /api/v1/entries` is offset+limit. The response carries `total`; the SPA increments `offset` by 50 to load more.

**Hazard**: when new entries arrive while the user is scrolling River, `offset += 50` skips or duplicates entries because new rows shift the position. At single-user scale this rarely bites. At many-feeds-and-active-polling scale, it will.

**Path forward**:
- Backend: extend `handleEntriesList` in `internal/api/entries.go` to accept a `cursor` query param of the form `<published_at>:<id>` (paired so it tie-breaks). Translate into `WHERE (published_at, id) < (?,?)` with the existing `ORDER BY published_at DESC, id DESC`. Return `next_cursor` (the last row's `published_at:id`) in the response.
- SPA: change `EntryList.onLoadMore` from `offset += limit` to `cursor = response.next_cursor`. Stop when `next_cursor` is `null`.
- Keep offset+limit working for backwards compat — both can coexist on the handler with a precedence rule (cursor wins).

## 2. DB-backed user preferences

**Today**: theme/font preferences live in `localStorage` keys (Phase 1b Unit 12b owns the keys). Per-browser only, no sync.

**Path forward**: a `user_preferences` table + `GET /api/v1/users/me/preferences` and `PUT /api/v1/users/me/preferences` handlers. Touchpoints:
- New migration in `internal/store/schema/`: `user_preferences (user_id, key, value)` keyed on the existing `defaultUserID = 1`.
- New handler in `internal/api/users.go` (the file doesn't exist yet — create it next to `feeds.go` and friends).
- SPA migration: on first run after the API exists, read localStorage, mirror to API, then read from API on subsequent loads.
- The new endpoint is also where to put feed-list sort order, river density, "mark read on scroll past" toggle, etc. — it shouldn't be a single-purpose theme/font endpoint.

## 3. JSON error envelope

**Today**: every handler uses `http.Error(w, msg, status)` which writes plain text. `web/src/lib/api.ts` `ApiError` falls back to status text — works, but the client can't programmatically tell "duplicate feed" apart from "validation error".

**Path forward**:
- Shared helper `writeError(w, code, msg, status)` in `internal/api/server.go` that writes `{ "error": { "code": "duplicate_feed", "message": "feed already subscribed" } }` with `Content-Type: application/json`.
- Sweep call sites: `git grep "http.Error" internal/api/`.
- Update `ApiError` in the SPA to parse `body.error.code` and `body.error.message`.
- Standardize codes: `validation_failed`, `not_found`, `duplicate`, `upstream_failure`, `internal_error`.
- Adopt when the SPA grows error handling that needs to branch on category (e.g., "URL already subscribed — go to that feed?" vs generic "couldn't add feed").

## 4. `unread_count` on single-resource endpoints

Phase 1b Unit 12-pre adds `unread_count` to `GET /api/v1/feeds` and `GET /api/v1/categories` (the *list* responses). Single-resource fetches `GET /feeds/:id` and `GET /categories/:id` still omit it. Worth filling in when Settings drilldowns need a live unread badge for a single feed/category page.

## 5. OPML import error reporting

**Today**: `POST /api/v1/opml/import` returns `{imported, skipped_duplicates, categories_created}`. If a feed entry can't be subscribed (bad URL, duplicate, network error), the count drops silently — the user has no way to know which.

**Path forward**: add a `failures: [{url: string, reason: string}]` array to the response. The SPA's import flow can then render a "couldn't import these N feeds" panel.

## 6. CI workflow polish (post-Unit-20)

Phase 1b Unit 20 ships a first-version `.github/workflows/ci.yml` (matrix Linux/macOS, `make test`, `npm ci && npm run build`, `docker build`, `golangci-lint run`). Future work:
- Upload the Linux+macOS `wire` binaries as workflow artifacts.
- Cache `~/go/pkg/mod` and `~/.npm` keyed on `go.sum` and `package-lock.json`.
- Add a CI status badge to the README.
- Codecov integration once test coverage is meaningful.
- Release workflow on tags (build artifacts, container image, GitHub release).

## How to use this list

Pick one item, scope it as a small PR, refer to "Path forward". None of these are urgent — they're each a couple-hour change once you decide the time is right.
