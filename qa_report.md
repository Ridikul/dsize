# QA Report — dsize 2.0.0 (web UI + scan history)

**Date:** 2026-06-27
**Platform under test:** macOS (darwin/arm64), Go toolchain ≥ 1.22
**Build:** `deploy.sh` cross-compiles darwin/{arm64,amd64}, linux/{amd64,arm64},
windows/amd64 — all succeed.

## Summary

✅ **PASS.** The code builds for all five target platforms, `go vet` is clean,
the full Go test suite is green, and a live end-to-end smoke test of the web UI
exercises every HTTP endpoint successfully. Two defects found during QA were
fixed (see below).

## Automated tests

```
$ cd src && go vet ./... && go test ./...
ok  	dsize/internal/history	0.4s
ok  	dsize/internal/server	0.9s
ok  	dsize/internal/sse	0.5s
?   	dsize/cmd/dsize     [no test files]
?   	dsize/internal/assets   [no test files]
?   	dsize/internal/scan     [no test files]
```

Coverage of note: `history` (snapshot read/write, traversal rejection, trend
computation), `server` (healthz, index, static assets, SSE complete event,
history list, path-traversal rejection, localhost binding), `sse` (event
encoding, 10 Hz progress throttle).

## Live smoke test (`dsize --ui`)

Started the release binary against a fixture directory and probed each endpoint:

| Endpoint | Result |
|----------|--------|
| `GET /healthz` | `200 {"status":"ok"}` |
| `GET /` | `200`, serves embedded `index.html` |
| `GET /events` (SSE) | streams `progress` then `complete` event |
| `GET /history?target=…` | `200`, JSON array of past snapshots |
| `GET /history/../../etc/passwd` (raw) | `400 Bad Request` (traversal blocked) |

History snapshots are persisted to
`~/Library/Application Support/dsize/history/` as atomic JSON files; CLI scans
also write snapshots. Verified clean runs leave no orphaned temp files.

CLI mode (`dsize`, `--top`, `--json`, `--bytes`, `--no-color`) is unchanged and
works as before.

## Defects found and fixed during QA

1. **SSE `/events` returned HTTP 500 when wrapped by logging middleware.**
   The `statusRecorder` used by `loggingMiddleware` embeds the
   `http.ResponseWriter` interface, which does not promote `Flush()`. The SSE
   writer's `http.Flusher` assertion therefore failed and the handler bailed
   out with 500. **Fix:** added a `Flush()` method to `statusRecorder` that
   forwards to the underlying `http.Flusher`. (`internal/server/server.go`)

2. **Path-traversal request returned 301 instead of 400.**
   `http.ServeMux` silently cleans `..` segments and issues a 301 redirect
   *before* the per-handler guard runs, bypassing it. **Fix:** added a
   traversal guard in `Server.ServeHTTP` that rejects any raw request path
   containing `..` with `400`, and routed the production server through
   `ServeHTTP` so the guard always applies. (`internal/server/server.go`)

3. **`go.mod` declared `go 1.21` but the code uses `http.FileServerFS`
   (Go 1.22+).** `go build` was lenient with a newer local toolchain, but
   `go vet` failed. **Fix:** bumped the module to `go 1.22` and updated the
   "Go 1.21+" references in README/INSTALL/deploy.sh. (`src/go.mod`)

4. **Results and History views never became visible in the browser.** The
   stylesheet sets `#results-view` and `#history-view` to `display: none`, but
   the JavaScript revealed them with `element.style.display = ''`. Clearing the
   inline style falls back to the stylesheet rule (`none`), so the panels
   stayed hidden even though the data and DOM nodes were present (the header
   counters updated, but the body was blank). **Fix:** set an explicit
   `display: 'block'` when showing those panels in `app.js`
   (`showResults` and the tab handler). Verified visually in Chrome: the
   colored size bars and the "size over time" history chart + table now
   render correctly.

## Known limitations / non-blocking notes

- Snapshot filenames have **per-second** granularity; two scans of the same
  target within the same second overwrite each other. Harmless for the
  intended use (tracking growth over hours/days).
- The browser-open step is best-effort; if it fails, the URL is printed to
  stderr so the user can open it manually.

## Sign-off

All acceptance criteria exercised pass. **Recommended for release.**
