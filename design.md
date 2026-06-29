# dsize Web UI — Design Document

**Version:** 1.0
**Date:** 2026-06-27
**Status:** Draft
**Source:** `requirements.md` v1.0

---

## 1. Design Goals & Constraints

- Add an **optional** `--ui` mode to the existing `dsize` binary with **zero behavioral regression** to the CLI path (FR-1.5, OOS-8).
- Preserve the **single static binary, no-CGO, no-runtime** property: all assets via `go:embed`, persistence as plain JSON files (FR-2.1, OOS-4, NFR-4.1).
- Localhost-only, single-user, ephemeral server — no auth, no daemon, no DB (OOS-1, OOS-5).
- Match the requirements as written; no speculative scaling, plugin systems, or extra storage backends.

---

## 2. Tech Stack

| Layer | Choice | One-line rationale |
|-------|--------|--------------------|
| Language | Go ≥ 1.21 | Existing codebase; stable `go:embed` and `os.UserCacheDir`/`UserConfigDir` (NFR-4.2). |
| HTTP server | stdlib `net/http` | Zero deps, sufficient for localhost single-user; no framework needed (OOS-1). |
| Asset embedding | `//go:embed` (`embed.FS`) | Keeps the single-binary guarantee, no CDN at runtime (FR-2.1). |
| Static serving | `http.FileServerFS` over the embedded FS | Built-in, handles caching/content-type for `/static/` (FR-2.2). |
| Streaming | SSE over `net/http` with `http.Flusher` | Native browser `EventSource`, no polyfills, simpler than WebSockets for one-way data (FR-3, NFR-4.3). |
| CLI flags | Existing flag parser (extend it) | Reuse current parsing to guarantee CLI parity (FR-1.5). |
| Persistence | JSON files in OS user-data dir | Mandated by FR-7 / OOS-4; human-inspectable, atomic via temp+rename. |
| Charting | Vendored **Chart.js** (single minified file, embedded) | Mature, canvas-based (meets 10k-entry/30fps target), no build toolchain, well under 5 MB (FR-2.4, NFR-1.4). |
| Frontend | Vanilla HTML/CSS/JS (no framework, no bundler) | No Node toolchain required (US-9); small footprint; SSE + DOM is all that's needed. |
| Browser open | `os/exec` of OS opener (`xdg-open`/`open`/`rundll32`) | Standard cross-platform pattern; degrades gracefully to printing URL (FR-1.4). |
| Tests | stdlib `testing` + `net/http/httptest` | Matches `go test ./...`, `-race`, `-cover` requirements (FR-9). |

**Explicitly rejected:** SQLite (OOS-4), web framework (overkill for localhost), WebSockets (one-way stream suffices), JS build pipeline (violates zero-runtime goal), WASM/SSR charts (OOS-7).

---

## 3. Module / Component Breakdown

### Backend (Go packages)

| Package | Responsibility | Depends on |
|---------|----------------|------------|
| `main` / `cmd/dsize` | Flag parsing, mode dispatch (CLI vs UI), port validation, mutual-exclusion checks, browser launch. | `internal/scan`, `internal/server`, `internal/history` |
| `internal/scan` (existing) | The disk walk; emits progress + entries; honors `--max-depth`/`--top`/`--bytes`. Extended with a **progress callback / channel** so UI can stream without changing CLI output. | stdlib only |
| `internal/server` | HTTP server: routes (`/`, `/static/`, `/events`, `/history`, `/history/<file>`, `/healthz`), SSE encoder, request logging, localhost bind. | `internal/scan`, `internal/history`, `internal/assets` |
| `internal/assets` | Holds `//go:embed` FS for `web/` (index.html, css, js, chart.js); exposes `fs.FS`. | `embed` |
| `internal/history` | Snapshot directory resolution per OS, atomic write, list/read summaries, full-snapshot read, **trend diff** vs latest prior snapshot. | stdlib only |
| `internal/sse` | SSE event types + serialization (`progress`/`complete`/`error`), 10 Hz throttle. | stdlib only |

### Frontend (`web/`, embedded)

| File | Responsibility |
|------|----------------|
| `index.html` | Single page: progress section, results section, history section, theme toggle. |
| `static/app.js` | `EventSource` wiring, DOM updates, view transition, client-side sort, tooltips, history fetch. |
| `static/charts.js` | Wraps Chart.js for the "size over time" line chart. |
| `static/style.css` | Layout, bars, `prefers-color-scheme` + manual dark-mode class, responsive 360–3840 px. |
| `static/chart.js` | Vendored Chart.js library (local, not CDN). |

---

## 4. Data Model

### Entity: `Entry`
| Field | Type | Notes |
|-------|------|-------|
| `path` | string | Absolute or scan-relative path. |
| `size` | int64 | Bytes. |
| `isDir` | bool | Directory vs file (drives bar styling FR-5.3). |
| `trend` | enum `up`\|`down`\|`equal`\|`new` | **Server-computed** in SSE `complete` only (FR-8.6); not persisted. |

### Entity: `Snapshot` (persisted JSON, FR-7.3)
| Field | Type | Notes |
|-------|------|-------|
| `version` | int | Schema version, currently `1`. |
| `scannedAt` | RFC3339 string | Scan completion time. |
| `target` | string | Absolute scanned path. |
| `totalSize` | int64 | Bytes. |
| `fileCount` | int | |
| `dirCount` | int | |
| `durationMs` | int64 | |
| `entries[]` | `[]{path,size,isDir}` | No `trend` persisted (trend is derived). |

### Entity: `SnapshotSummary` (`/history` response, FR-8.4)
All `Snapshot` fields **except** `entries[]`, plus derived `deltaBytes`/`deltaPct` may be computed client-side from the ordered list.

### Relationships
- One `target` (directory) → many `Snapshot`s (one per scan), keyed/grouped by `target`.
- Snapshots for a target form a time series → drives the line chart and trend diff.
- File naming: `<ISO8601 with ':'→'-'>_<base64url(target)>.json` (FR-7.2) — encodes the target→snapshot link in the filename for `/history?target=` filtering without reading every file.

### Storage location (FR-7.1)
Resolved per-OS (Linux `~/.local/share/dsize/history/`, macOS `~/Library/Application Support/dsize/history/`, Windows `%APPDATA%\dsize\history\`), created `0700` if absent.

---

## 5. Public API Surface

### CLI (additions only; existing flags unchanged — FR-1.5)
```
dsize [existing flags] [path]
  --ui                 Start local HTTP UI server, open browser, suppress stdout text.
  --ui-port <int>      Port 1–65535 (default 8420). Invalid → stderr "invalid port", exit 1.
```
- `--ui` + `--json` or `--ui` + `--no-color` → stderr "mutually exclusive", exit 1 (FR-1.6).
- `--ui` composes with `--max-depth`, `--top`, `--bytes`.

### HTTP endpoints (bind `127.0.0.1:<port>` only — NFR-2.1)
| Method | Path | Returns |
|--------|------|---------|
| GET | `/` | `index.html` (text/html). |
| GET | `/static/*` | Embedded assets incl. `chart.js`. |
| GET | `/events` | `text/event-stream`, `Cache-Control: no-cache`, `retry: 3000`; SSE `progress`/`complete`/`error`. |
| GET | `/history?target=<encoded>` | JSON array of `SnapshotSummary` (no entries), ascending `scannedAt`. |
| GET | `/history/<filename>` | Full `Snapshot`. `..`/traversal → **400** (NFR-2.2). |
| GET | `/healthz` | `200 {"status":"ok"}`. |

### Key Go signatures (internal)
```go
// internal/history
func HistoryDir() (string, error)
func Write(s Snapshot) error                       // atomic temp+rename; never fatal to caller
func ListSummaries(target string) ([]Summary, error)
func ReadSnapshot(filename string) (Snapshot, error) // rejects traversal
func ComputeTrends(curr, prev []Entry) []Entry       // sets trend field

// internal/sse
type Event struct { Type string; Data any }
func (w *Writer) Emit(e Event) error                 // throttles progress to ≤10Hz
func (w *Writer) Flush()

// internal/server
func New(scanResult <-chan scan.Progress, hist HistoryStore, assets fs.FS) *Server
func (s *Server) Start(port int) (url string, err error)

// internal/scan (extension)
func ScanStream(ctx, path string, opts Options, onProgress func(Progress)) (Result, error)
```

---

## 6. Main Use-Case Flow (US-1: `dsize --ui /path`)

```
User                CLI(main)            server          scan engine        history          Browser
 |  dsize --ui /path  |                     |                 |                 |                 |
 |------------------->| validate flags/port |                 |                 |                 |
 |                    | (exit 1 on bad)     |                 |                 |                 |
 |                    |--start server------>| bind 127.0.0.1  |                 |                 |
 |                    |<---ready (url)------ |                 |                 |                 |
 |                    |--open browser-------------------------------------------------------------->| GET /
 |                    |                     |---index.html-------------------------------------------->|
 |                    |                     |<--GET /events (EventSource)-----------------------------|
 |                    |--run scan---------->|                 |                 |                 |
 |                    |                     |  onProgress --> SSE progress (≤10Hz) ----------------->| live view
 |                    |                     |  scan done                        |                 |
 |                    |                     |--load latest prior snapshot------>| ComputeTrends   |
 |                    |                     |<--prev entries--------------------|                 |
 |                    |                     |  SSE complete{entries,trends} ----------------------->| results view
 |                    |--write snapshot (atomic)---------------------------------->| temp+rename   |
 |                    |   (warn+continue on failure, exit 0)                       |               |
 | History tab click  |                     |<--GET /history?target=... ---------------------------- |
 |                    |                     |--summaries (asc scannedAt)--------------------------->| line chart + table
```

**Concurrency model:** scan runs in its own goroutine; a single in-memory progress broadcaster fans out to ≥5 SSE subscribers via per-client buffered channels (NFR-1.1). The 10 Hz throttle is applied at the broadcaster source so slow clients drop intermediate frames, never block the walk.

**CLI path (US-8, no `--ui`):** flag dispatch skips `internal/server` entirely; scan → existing text/JSON output → snapshot write. Byte-for-byte stdout parity is preserved by not touching the existing print path (AC-1.1).

---

## 7. Cross-Cutting Concerns

- **Security:** localhost-only bind; `/history/<file>` resolves via `filepath.Clean` + prefix check against the history dir, rejecting any `..` with 400; paths base64url-encoded in filenames; no shell exec of user input (NFR-2).
- **Observability:** middleware logs method/path/status/latency to stderr; walk errors counted into `errorCount` on the `complete` event; `/healthz` for readiness (NFR-3).
- **Atomicity/robustness:** snapshot write = `os.CreateTemp` in history dir → write → `fsync` → `os.Rename`; any failure logs a warning and the process still exits 0 (FR-7.5/7.6).
- **Lifecycle:** server is ephemeral; Ctrl-C or process exit stops it (OOS-5). No retention/pruning (OOS-9).

---

## 8. Risks & Mitigations

| # | Risk | Impact | Mitigation |
|---|------|--------|-----------|
| R-1 | Embedded Chart.js + assets push binary growth past 5 MB (NFR-1.4). | Violates size constraint. | Vendor only minified Chart.js; CI check asserts asset dir < 5 MB; trim unused chart types if needed. |
| R-2 | 10,000-entry results DOM kills frame rate (NFR-1.2). | Janky UI. | Render bars from a single canvas/virtualized list; avoid one heavy DOM node per entry; sort in JS arrays not via reflow. |
| R-3 | SSE progress flooding slow clients degrades scan throughput (NFR-1.1). | Scan slowdown. | Throttle at source ≤10Hz; non-blocking per-client buffered channels that drop stale progress frames. |
| R-4 | Path-traversal on `/history/<file>` exposes arbitrary files (NFR-2.2). | Security breach. | Clean+confine to history dir, reject `..`, unit + integration test (AC-8.4). |
| R-5 | Any UI-path change leaks into CLI output and regresses AC-1.1. | Broken existing workflows. | Keep CLI print path untouched; golden-file regression test diffing stdout against pre-feature binary. |
| R-6 | Browser auto-open fails on headless/unusual envs (FR-1.4). | User confused, no UI. | Best-effort open; always print URL to stderr on failure; server keeps running. |
| R-7 | Race conditions across scan/broadcast/SSE goroutines (FR-9.3). | Flaky/corrupt output. | Channel-based ownership (no shared mutable state); CI `go test -race`. |
| R-8 | Filename timestamp collisions for rapid successive scans of same target. | Snapshot overwrite. | ISO8601 to seconds + atomic rename; acceptable per single-user model; second granularity sufficient for manual scans. |
| R-9 | Trend diff cost on large entry sets (FR-8.6). | Slow `complete`. | Diff via map keyed on path, O(n); reuse already-loaded prior snapshot only (latest), not full history. |
| R-10 | History dir unwritable / read-only FS (FR-7.6). | Crash risk. | Treat as warning-only; never fatal; covered by AC-7.3. |

---

## 9. Test Strategy Mapping (FR-9)

- **Unit:** snapshot write/read round-trip, `ComputeTrends`, SSE serialization + throttle, per-OS history-dir resolution, port validation.
- **Integration (`httptest`):** server start, `complete` SSE for a fixture dir, `/history` correctness, `/history/..` → 400, `/healthz`.
- **Regression:** golden stdout for non-`--ui` runs (AC-1.1).
- Gates: `go test -race ./...` green; `-cover` ≥ 80% on `internal/ui|server` + `internal/history`.

---

*Design constrained strictly to requirements.md v1.0 — no additional persistence backends, auth, daemon mode, or scaling beyond the stated 5-client / 10k-entry / 100k-snapshot targets.*
