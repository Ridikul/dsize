# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/), and this project adheres
to semantic versioning.

## [2.1.0] — 2026-06-29

### Added
- `install.sh` / `install.ps1` and prebuilt release binaries for one-line
  install on macOS, Linux and Windows.
- The scanned directory's total size is now shown prominently in the results
  view (a large highlighted figure), with the file/dir count and scan duration
  moved to a secondary line.

### Changed
- Clearer per-row actions: the icon-only hover buttons now carry text labels
  (Scan / Open / Reveal) and stay faintly visible so they're discoverable.
- Sort buttons show the active sort direction (↑/↓); counts are pluralized
  correctly (e.g. "1 file · 1 dir").
- Folder picker in the web UI, always available from the header. `dsize --ui`
  may now be launched without a path: a welcome screen lets you choose a folder
  via the native OS dialog (`osascript` / `zenity` / PowerShell), with a
  path-field fallback when no dialog tool is present. New endpoints: `GET /state`
  (current root) and `POST /pick` (open dialog, or `?path=` to set directly);
  the chosen folder becomes the new scan root and breadcrumb floor.
- Per-row actions in the web UI, revealed on hover: open a directory in the OS
  file manager, re-scan a directory as the new root, or reveal a file
  (selected) in its containing folder. Backed by a new `POST /open` endpoint
  and an optional `?root=` parameter on `/events`; both are restricted to paths
  within the original scan root.
- Breadcrumb of the current scan root: click any ancestor segment to re-scan at
  that level, down to the original root (the navigation floor). The `complete`
  event now also carries `base` (the startup root).

### Fixed
- Scanning `/` on macOS reported far more than the disk holds, from three
  causes, now all addressed:
  - **Logical vs. on-disk size** — dsize summed logical file sizes; it now
    reports real allocated disk usage (`st_blocks`), like `du`, matching the
    Storage pane. macOS system files are heavily compressed/cloned, so the
    logical size is much larger than the space used (e.g. `/usr` 1.5 GiB
    logical vs 0.75 GiB on disk).
  - **Duplicate counting** — de-duplicate by `(device, inode)` so APFS
    firmlinks (`/Users` vs `/System/Volumes/Data/Users`) and ordinary
    hardlinks are counted once.
  - **Crossing volumes** — stay on the scan root's filesystem by default
    (like `du -x`), excluding swap, Preboot, Update and other mounted volumes.

### Added
- `--cross-mounts` flag to descend into other mounted filesystems (restores
  the pre-fix cross-volume behavior).
- `--apparent-size` flag to sum logical file sizes instead of on-disk usage.

## [2.0.0] — 2026-06-27

### Added
- **Local web UI** (`--ui`): starts an HTTP server on `127.0.0.1:8420` and
  opens the default browser to a live view of the scan.
  - Real-time progress over Server-Sent Events: overall progress bar, current
    path, and running counters (total size, file count, dir count, elapsed
    time, files/sec).
  - Largest entries rendered as colored, size-proportional bars with per-entry
    trend indicators (up / down / equal / new) versus the previous scan.
  - "Size over time" line chart and a run-history table (date, total, delta vs
    previous, file count, duration).
  - Dark mode; the whole frontend is embedded in the binary via `go:embed`.
- **`--ui-port`** flag to choose the UI server port (default `8420`).
- **Scan history**: every CLI and UI scan is persisted as a JSON snapshot in
  the OS-appropriate data directory (`~/Library/Application Support/dsize` on
  macOS, `$XDG_DATA_HOME/dsize` on Linux, `%APPDATA%\dsize` on Windows).
  Snapshots are written atomically (temp file + rename).
- Local-only HTTP endpoints: `/events` (SSE), `/history`, `/history/<file>`,
  `/healthz`, and embedded static assets.

### Security
- History file endpoint rejects path traversal: requests whose path contains
  `..` are answered with `400 Bad Request` before reaching the file handler.

### Changed
- The classic CLI behavior is unchanged. `--ui` is purely additive and is
  mutually exclusive with `--json` and `--no-color`.

## [1.0.0] — prior release

### Added
- Recursive directory scanner listing files and folders by size, largest
  first, with human-readable IEC units.
- Flags: `--max-depth`, `--top`, `--bytes`, `--json`, `--no-color`.
- Cross-platform single static binary (macOS, Linux, Windows).
