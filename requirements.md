# dsize Web UI — Requirements Document

**Version:** 1.0  
**Date:** 2026-06-27  
**Status:** Draft

---

## Project Summary

`dsize` is an existing cross-platform disk-usage scanner distributed as a single static Go binary. This project adds an optional `--ui` flag that starts a local HTTP server, streams live scan progress to a browser via Server-Sent Events, and renders results as interactive, size-proportional bar charts with historical trend indicators. All frontend assets are embedded directly in the binary via `go:embed`, preserving the zero-dependency, single-executable property; run history is persisted as JSON files in the OS-appropriate user data directory.

---

## User Stories

| # | Role | Goal | Benefit |
|---|------|------|---------|
| US-1 | CLI power user | Launch `dsize --ui /path` and have a browser open automatically | See scan progress and results visually without learning new tooling |
| US-2 | Developer | Bind the UI to a custom port via `--ui-port` | Avoid port conflicts when running multiple instances |
| US-3 | End user | Watch a live progress bar, current path, file/dir counts, and files/sec while the scan runs | Know the scan is working and how far along it is |
| US-4 | End user | View final results as colored, size-proportional bars when the scan completes | Quickly identify the largest consumers of disk space |
| US-5 | Returning user | See an up/down/equal trend arrow on each entry versus the previous run | Understand what has grown or shrunk since last time |
| US-6 | Analyst | View a "size over time" line chart of total disk usage across all historical runs | Spot long-term disk growth trends |
| US-7 | Analyst | Browse a run-history table listing date, total size, delta vs previous, file count, and duration | Compare runs at a glance without re-scanning |
| US-8 | System administrator | Use `dsize` without `--ui` and get identical behavior to today | Trust that the new feature has not regressed existing workflows |
| US-9 | User on any OS | Install dsize as a single binary on Linux, macOS, or Windows | Zero friction; no Node, Python, or other runtime required |
| US-10 | User | Toggle between light and dark mode in the browser | Comfortable viewing in any lighting condition |

---

## Functional Requirements

### FR-1 — New `--ui` flag

**FR-1.1** `dsize` MUST accept a `--ui` boolean flag. When present, the program starts an HTTP server before beginning the scan and does NOT emit any text output to stdout/stderr (except fatal errors).

**FR-1.2** When `--ui` is passed without `--ui-port`, the server MUST bind to `localhost:8420`.

**FR-1.3** `dsize` MUST accept `--ui-port <int>` to override the default port. Valid range: 1–65535. Passing an invalid value MUST print an error and exit with code 1.

**FR-1.4** After the server is ready, `dsize` MUST open the user's default browser to `http://localhost:<port>/`. If the browser cannot be opened, the server MUST still run and the URL MUST be printed to stderr.

**FR-1.5** When `--ui` is NOT passed, all existing flags (`--json`, `--top`, `--max-depth`, `--bytes`, `--no-color`) MUST behave identically to the current implementation. No behavioral regression is permitted.

**FR-1.6** `--ui` MUST be compatible with `--max-depth`, `--top`, and `--bytes`; the scan engine respects these filters and the UI reflects them. `--ui` is mutually exclusive with `--json` and `--no-color` (error + exit 1 if combined).

---

### FR-2 — Embedded Frontend Assets

**FR-2.1** All frontend files (HTML, CSS, JavaScript, and any vendored JS libraries such as Chart.js) MUST be embedded in the Go binary using the `//go:embed` directive. No external CDN requests are permitted at runtime.

**FR-2.2** The embedded assets MUST be served by the HTTP server under the path `/static/`.

**FR-2.3** A single `index.html` MUST be served at `/`.

**FR-2.4** Chart.js (or an equivalent charting library) MUST be bundled as a local file under the embedded assets, not loaded from a CDN.

---

### FR-3 — Server-Sent Events (SSE) Streaming

**FR-3.1** The server MUST expose a GET `/events` endpoint that streams SSE messages to the browser while the scan is running.

**FR-3.2** Each SSE event MUST carry a `type` field. Required event types:

| Event type | Payload fields |
|------------|---------------|
| `progress` | `currentPath`, `totalSize`, `fileCount`, `dirCount`, `elapsedMs`, `filesPerSec` |
| `complete` | `totalSize`, `fileCount`, `dirCount`, `elapsedMs`, `entries[]` |
| `error` | `message` |

**FR-3.3** `progress` events MUST be emitted at a rate of at most 10 Hz (one event per 100 ms minimum interval) to avoid flooding slow connections.

**FR-3.4** `entries[]` in the `complete` event MUST be an array of objects with fields: `path`, `size` (bytes), `isDir` (bool), `trend` (`"up"` | `"down"` | `"equal"` | `"new"`).

**FR-3.5** The SSE connection MUST send a `retry: 3000` line so the browser reconnects after 3 s if the connection drops before `complete` is received.

**FR-3.6** The server MUST respond with `Content-Type: text/event-stream` and `Cache-Control: no-cache` headers on `/events`.

---

### FR-4 — Live Progress View

**FR-4.1** While the scan is running the browser page MUST display:
  - An animated progress bar (indeterminate, since total is unknown upfront).
  - The full path of the file/directory currently being scanned.
  - Running counters: total size (human-readable), file count, dir count, elapsed time (HH:MM:SS), files/sec.

**FR-4.2** All counters MUST update in real time as SSE `progress` events arrive; updates MUST be visible within 200 ms of event receipt (client-side rendering constraint).

**FR-4.3** The page title MUST update to reflect the scanned directory (e.g., `dsize — /home/user`).

---

### FR-5 — Results View

**FR-5.1** Upon receiving the `complete` SSE event, the progress view MUST transition to the results view without a full page reload.

**FR-5.2** The results view MUST render each entry in `entries[]` as a horizontal bar whose width is proportional to its size relative to the largest entry. Minimum visible width: 4 px.

**FR-5.3** Bars for directories MUST be visually distinct from bars for files (e.g., different hue or pattern).

**FR-5.4** Each bar MUST display: entry name/path, human-readable size, and a trend indicator icon (▲ up / ▼ down / = equal / ★ new).

**FR-5.5** The results view MUST be sorted descending by size by default. Clicking a column header (Name, Size, Trend) MUST re-sort the table in the browser without a server round-trip.

**FR-5.6** Hovering over a bar MUST show a tooltip with: absolute path, exact byte count, and percentage of total.

---

### FR-6 — Dark Mode

**FR-6.1** The UI MUST default to the user's OS color scheme preference via the CSS `prefers-color-scheme` media query.

**FR-6.2** A toggle button MUST allow the user to manually switch between light and dark mode within the session.

**FR-6.3** The chosen mode MUST be persisted in `localStorage` and restored on reload.

---

### FR-7 — Run History Persistence

**FR-7.1** After each successful scan (whether or not `--ui` is active), `dsize` MUST write a JSON snapshot file to the OS-appropriate user data directory:
  - Linux: `~/.local/share/dsize/history/`
  - macOS: `~/Library/Application Support/dsize/history/`
  - Windows: `%APPDATA%\dsize\history\`

**FR-7.2** Each snapshot filename MUST be `<ISO8601-timestamp>_<base64url-encoded-target-path>.json` (e.g., `2026-06-27T09-17-58Z_L2hvbWUvdXNlcg.json`). Colons in the timestamp MUST be replaced with hyphens for filesystem compatibility.

**FR-7.3** The snapshot JSON schema MUST include:

```json
{
  "version": 1,
  "scannedAt": "<RFC3339>",
  "target": "<absolute path>",
  "totalSize": 0,
  "fileCount": 0,
  "dirCount": 0,
  "durationMs": 0,
  "entries": [
    { "path": "", "size": 0, "isDir": false }
  ]
}
```

**FR-7.4** The history directory MUST be created automatically if it does not exist (mode 0700 on Unix).

**FR-7.5** Snapshot writes MUST be atomic: write to a temp file first, then rename. A failed write MUST NOT corrupt existing history files.

**FR-7.6** `dsize` MUST NOT fail or exit non-zero if it cannot write the snapshot (e.g., read-only filesystem); it MUST log a warning to stderr and continue.

---

### FR-8 — Historical Analytics in the UI

**FR-8.1** The UI MUST include a "History" tab/section accessible from the results view.

**FR-8.2** The History section MUST display a line chart of total scanned size (Y-axis, human-readable) over scan date (X-axis) for all historical runs of the same target directory.

**FR-8.3** The History section MUST display a table of past runs with columns: Date, Total Size, Δ vs Previous (bytes and %), File Count, Duration. Rows MUST be sorted descending by date.

**FR-8.4** The server MUST expose GET `/history?target=<encoded-path>` returning a JSON array of past snapshot summaries (all fields except `entries[]`) sorted ascending by `scannedAt`.

**FR-8.5** The server MUST expose GET `/history/<filename>` to retrieve a full snapshot including `entries[]` for drill-down.

**FR-8.6** Per-entry trend indicators (FR-3.4 / FR-5.4) MUST be computed server-side by diffing the current scan's entries against the most recent prior snapshot for the same target.

---

### FR-9 — Tests

**FR-9.1** Unit tests MUST cover: snapshot write/read round-trip, trend computation logic, SSE event serialization, history directory resolution per OS, and port validation.

**FR-9.2** Integration tests MUST cover: starting the HTTP server, receiving a `complete` SSE event for a fixture directory, verifying the `/history` endpoint returns correct data.

**FR-9.3** Tests MUST pass with `go test ./...` and produce no race conditions when run with `-race`.

**FR-9.4** Test coverage of new packages MUST be ≥ 80% (measured by `go test -cover`).

---

### FR-10 — Documentation & Install

**FR-10.1** A `INSTALL.md` MUST document per-OS installation steps for Linux, macOS (Homebrew + manual), and Windows (manual + Scoop if applicable).

**FR-10.2** `INSTALL.md` MUST include a "Quick start" section showing `dsize --ui /path/to/dir`.

**FR-10.3** `README.md` (or an addendum section) MUST document all new flags (`--ui`, `--ui-port`) and describe the history storage location per OS.

---

## Non-Functional Requirements

### NFR-1 — Performance

**NFR-1.1** The HTTP server MUST handle at least 5 concurrent SSE client connections without degrading scan throughput by more than 5%.

**NFR-1.2** The results page MUST render up to 10,000 entries without dropping below 30 fps on a 2020-era mid-range laptop (Chrome/Firefox).

**NFR-1.3** Snapshot JSON files MUST be written in under 2 seconds for scans returning up to 100,000 entries.

**NFR-1.4** The binary size increase due to embedded frontend assets MUST NOT exceed 5 MB (uncompressed).

### NFR-2 — Security

**NFR-2.1** The HTTP server MUST bind exclusively to `localhost` (127.0.0.1); it MUST NOT bind to `0.0.0.0` or any external interface.

**NFR-2.2** The `/history` and `/history/<filename>` endpoints MUST validate that the requested filename is confined to the history directory (no path traversal). Any `..` component MUST result in HTTP 400.

**NFR-2.3** The server MUST NOT execute any user-supplied shell commands. SSE output is read-only data.

**NFR-2.4** Snapshot filenames and their contents MUST NOT embed raw user-controlled strings without sanitization (use base64url encoding for paths in filenames per FR-7.2).

### NFR-3 — Observability

**NFR-3.1** The server MUST log (to stderr) each HTTP request with method, path, status code, and latency.

**NFR-3.2** Scan errors (permission denied, broken symlinks) encountered during the walk MUST be counted and included in the `complete` event payload as `errorCount`.

**NFR-3.3** The server MUST expose GET `/healthz` returning HTTP 200 `{"status":"ok"}` for readiness checks.

### NFR-4 — Compatibility

**NFR-4.1** The implementation MUST compile and produce a working binary on Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64) using standard `go build` with no CGO.

**NFR-4.2** The minimum supported Go version is **1.21** (required for `go:embed` stability and `os.UserCacheDir` availability).

**NFR-4.3** The frontend MUST function in the latest two major versions of Chrome, Firefox, and Safari without polyfills for SSE (all natively support `EventSource`).

### NFR-5 — Usability

**NFR-5.1** The UI MUST be responsive and usable at viewport widths from 360 px to 3840 px.

**NFR-5.2** Human-readable sizes MUST use IEC units (KiB, MiB, GiB, TiB) when `--bytes` is not passed; raw bytes when it is.

---

## Out of Scope

| # | Non-Goal |
|---|----------|
| OOS-1 | Authentication or multi-user access control on the HTTP server |
| OOS-2 | Remote/networked scanning (scanning paths on other machines via SSH or UNC) |
| OOS-3 | Editing, deleting, or moving files from the UI |
| OOS-4 | A relational database, SQLite, or any non-JSON persistence backend |
| OOS-5 | A dedicated daemon or background service mode (the server exits when the browser tab closes or Ctrl-C is pressed) |
| OOS-6 | Real-time file-system watching / auto-rescan after the initial scan completes |
| OOS-7 | WebAssembly or server-side rendering of charts; all charting is client-side JS |
| OOS-8 | Changes to `--json`, `--top`, `--max-depth`, `--bytes`, or `--no-color` behavior |
| OOS-9 | History pruning / retention policy (no automatic deletion of old snapshots) |
| OOS-10 | Mobile-native apps or Electron packaging |

---

## Acceptance Criteria

### AC for FR-1 (--ui flag)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-1.1 | `dsize /tmp` (no `--ui`) produces identical stdout to the pre-feature binary | Byte-for-byte stdout match in regression test |
| AC-1.2 | `dsize --ui /tmp` starts a server and opens a browser | Browser opens; `GET /` returns HTTP 200 within 3 s |
| AC-1.3 | `dsize --ui --ui-port 9999 /tmp` binds to port 9999 | `curl http://localhost:9999/` returns HTTP 200 |
| AC-1.4 | `dsize --ui --ui-port 99999 /tmp` exits with code 1 | Process exits 1; stderr contains "invalid port" |
| AC-1.5 | `dsize --ui --json /tmp` exits with code 1 | Process exits 1; stderr contains "mutually exclusive" |

### AC for FR-2 (Embedded Assets)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-2.1 | Binary contains embedded assets | `strings dsize \| grep "Chart.js"` matches (or equivalent) |
| AC-2.2 | `GET /static/chart.js` returns HTTP 200 without internet | Verified with network disconnected |
| AC-2.3 | `GET /` returns valid HTML with `<html>` tag | HTTP 200, Content-Type text/html |

### AC for FR-3 (SSE)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-3.1 | `GET /events` returns `Content-Type: text/event-stream` | Header present in response |
| AC-3.2 | At least one `progress` event is received during a scan of a directory with ≥ 10 files | Event stream contains `event: progress` line |
| AC-3.3 | A `complete` event is received after scan finishes | Event stream contains `event: complete` followed by `data: {...}` |
| AC-3.4 | `complete` payload parses as valid JSON with all required fields | JSON.parse succeeds; all fields present |
| AC-3.5 | Progress events are not emitted faster than 10 Hz | Timestamp diff between consecutive events ≥ 100 ms |

### AC for FR-4 (Live Progress View)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-4.1 | Progress bar is visible while scan runs | DOM element with role="progressbar" or class="progress" is present |
| AC-4.2 | Current path updates during scan | `currentPath` element text changes at least once during a 50-file scan |
| AC-4.3 | File count, total size, elapsed time display during scan | All three DOM elements contain non-zero values before scan ends |

### AC for FR-5 (Results View)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-5.1 | Results view replaces progress view after `complete` | Progress bar disappears; bar chart appears; no page reload (no navigation event) |
| AC-5.2 | Largest entry bar is 100% width | CSS width of widest bar element equals container width |
| AC-5.3 | All entries show name, size, trend icon | Each row has non-empty text in name, size, trend cells |
| AC-5.4 | Clicking "Size" header reverses sort order | After click, smallest entry appears last |
| AC-5.5 | Tooltip appears on hover | Mouseover triggers element with absolute path and byte count |

### AC for FR-6 (Dark Mode)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-6.1 | Page loads in dark mode when OS prefers dark | With `prefers-color-scheme: dark` emulated, body background is dark (luminance < 50%) |
| AC-6.2 | Toggle button switches mode | After click, background luminance changes |
| AC-6.3 | Mode persists on reload | `localStorage.getItem('colorScheme')` is set; mode survives `location.reload()` |

### AC for FR-7 (History Persistence)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-7.1 | Snapshot file created after scan | File matching `*.json` exists in history dir after `dsize --ui /tmp` completes |
| AC-7.2 | Snapshot parses as valid JSON with all required fields | `jq .version` returns `1`; all schema fields present |
| AC-7.3 | Write failure does not crash dsize | With read-only history dir, dsize exits 0; stderr contains "warning" |
| AC-7.4 | Temp-file-then-rename atomicity | No partial JSON file remains if process is killed during write (verified by inspection) |

### AC for FR-8 (Historical Analytics)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-8.1 | History tab renders a line chart | `<canvas>` element is present in History section with Chart.js instance attached |
| AC-8.2 | `GET /history?target=<path>` returns JSON array | HTTP 200; response is a JSON array; each element has `scannedAt` and `totalSize` |
| AC-8.3 | Run history table shows correct delta | With two snapshots differing by 1 MiB, delta column shows "+1.0 MiB" |
| AC-8.4 | Path traversal rejected | `GET /history/../../etc/passwd` returns HTTP 400 |
| AC-8.5 | Trend indicators match diff | Entry that grew since last run shows ▲; shrunken entry shows ▼ |

### AC for FR-9 (Tests)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-9.1 | `go test ./...` passes with no failures | Exit code 0 |
| AC-9.2 | `go test -race ./...` passes with no races | Exit code 0; no "DATA RACE" in output |
| AC-9.3 | Coverage ≥ 80% for new packages | `go test -cover ./internal/ui/... ./internal/history/...` reports ≥ 80% |

### AC for NFR-1 (Performance)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-P1 | 5 concurrent SSE clients do not degrade scan throughput > 5% | Benchmark: scan time with 5 clients ≤ 1.05× scan time with 0 clients |
| AC-P2 | 10,000-entry results page renders smoothly | Chrome DevTools Performance panel shows no frames < 33 ms during initial render |

### AC for NFR-2 (Security)

| ID | Criterion | Pass condition |
|----|-----------|---------------|
| AC-S1 | Server does not bind to 0.0.0.0 | `ss -tlnp` / `netstat` shows only 127.0.0.1 listening |
| AC-S2 | Path traversal blocked on /history | `curl http://localhost:8420/history/../../etc/passwd` → HTTP 400 |
