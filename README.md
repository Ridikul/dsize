# dsize — Directory Size Scanner with Web UI

Recursively scan a directory tree and list every file and folder sorted by
size, largest first, with human-readable units. Works identically on macOS,
Linux, and Windows. Single static binary — no runtime required.

New in this release: a **local web UI** (`--ui`) that shows the scan running
live — animated progress, running counters, colored size bars — and keeps a
**history of past scans** so you can watch how your folders grow over time on
an interactive chart.

## Install

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/Ridikul/dsize/main/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/Ridikul/dsize/main/install.ps1 | iex
```

See **[INSTALL.md](INSTALL.md)** for manual download, checksums, the unsigned-binary
note (Gatekeeper/SmartScreen), and building from source.

## Quickstart

```bash
# Classic CLI — unchanged
dsize .

# Live web UI in your browser
dsize --ui ~/Documents

# …or launch the UI with no folder and pick one from the browser
dsize --ui
```

`dsize --ui` starts a small HTTP server bound to `127.0.0.1:8420`, opens your
default browser, and streams the scan to the page in real time. Press
`Ctrl-C` in the terminal to stop the server.

## The web UI

```
 ┌───────────────────────────────────────────────────────────────┐
 │  dsize        http://127.0.0.1:8420                    ⚙  🌙    │
 │  ~/Documents        ▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░  82%  scanning Projects…  │
 │  Total 6.4 GiB ▲+4%   Files 12 480   Dirs 1 204   3.8s          │
 │  ████████████████  Projects   3.1G ▲   ┌ Size over time ─────┐  │
 │  ████████████      Photos     2.4G ▲   │        ╭───●        │  │
 │  ████              Music      820M =   │   ╭───╯             │  │
 │  ██                backup.tgz 512M ▼   └────────────────────┘  │
 └───────────────────────────────────────────────────────────────┘
```

- **Live progress** streamed over Server-Sent Events: overall progress bar,
  current path, running totals (size, files, dirs, elapsed, files/sec).
- **Colored, size-proportional bars** for the largest entries, with per-entry
  trend arrows (▲ grew · ▼ shrank · = unchanged · new) versus the previous scan.
- **"Size over time" chart** and a **run-history table** (date, total, delta vs
  previous, file count, duration) powered by the local history store.
- **Per-row actions** (revealed on hover): a directory can be **opened** in the
  OS file manager or **re-scanned** as the new root; a file can be **revealed**
  (selected) in its folder. Re-scans stay within the original scan root.
- **Breadcrumb** of the current scan root: click any ancestor segment to
  re-scan at that level, up to the original root (the navigation floor).
- **Folder picker**, always available from the header: launch `dsize --ui`
  with no path and a welcome screen lets you choose a folder via the native OS
  dialog (with a path-field fallback where no dialog tool is available).
- **Dark mode** and a tasteful color palette; the entire frontend is embedded
  in the binary via `go:embed`, so there is still nothing to install.

Choose a different port with `--ui-port`:

```bash
dsize --ui --ui-port 9000 ~/code
```

## Scan history

Every scan — CLI or UI — is saved as a small JSON snapshot under your
OS-appropriate data directory:

| OS | Location |
|----|----------|
| macOS | `~/Library/Application Support/dsize/history/` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/dsize/history/` |
| Windows | `%APPDATA%\dsize\history\` |

Snapshots are plain JSON (one file per scan, named by timestamp + target) — no
database, no extra dependency. Delete the directory anytime to reset history.

## CLI usage (unchanged)

```bash
# Top 10 items, 2 levels deep
dsize ~/Documents --max-depth 2 --top 10

# Raw byte counts (pipe-friendly)
dsize /var/log --bytes

# JSON — pipe to jq
dsize . --json | jq '.entries[] | select(.isDir == false) | .path'

# Disable ANSI color
dsize . --no-color
```

## All flags

| Flag | Description |
|------|-------------|
| `--ui` | Start the local web UI server and open the browser |
| `--ui-port N` | Port for the UI server (default `8420`, range 1–65535) |
| `--max-depth N` | Max directory depth (`0` = unlimited) |
| `--top N` | Show only the N largest entries (`0` = all) |
| `--bytes` | Output raw integer byte counts (no unit suffix) |
| `--json` | Output a JSON object to stdout |
| `--no-color` | Disable ANSI color in CLI output |
| `--cross-mounts` | Descend into other mounted filesystems (default: stay on the root's filesystem, like `du -x`) |
| `--apparent-size` | Sum logical file sizes instead of on-disk allocated size (default: real disk usage, like `du`) |

`--ui` is mutually exclusive with `--json` and `--no-color`.

**Exit codes:** `0` success · `1` bad path, invalid port, or conflicting flags

### Mounted volumes and hardlinks

By default dsize stays on the scan root's filesystem (like `du -x`) and counts
each inode once. This matters most when scanning `/` on macOS: the data volume
is firmlinked into `/` (so `/Users` and `/System/Volumes/Data/Users` are the
same files), and swap/Preboot/Update live on separate mounted volumes. Without
de-duplication and mount-boundary handling, `dsize /` reports far more than the
disk actually holds. Pass `--cross-mounts` to include other mounted filesystems.

dsize also reports **real on-disk usage** (allocated blocks) by default, like
`du`, so the totals match Finder/the Storage settings. macOS compresses system
files and uses APFS clones, so their logical size is much larger than the space
they occupy; pass `--apparent-size` to sum logical sizes instead.

## Installation

See **[INSTALL.md](INSTALL.md)** for pre-built binaries and per-OS steps.
Build from source with Go 1.22+:

```bash
make install          # build and copy onto your PATH (uses sudo if needed)
# or just build, without installing:
make build            # produces src/dsize
```

`make install` targets `/opt/homebrew/bin` on Apple Silicon, else
`/usr/local/bin`; override with `make install BINDIR=~/bin`. Plain `go build`
also works: `cd src && go build -o dsize ./cmd/dsize`.

## Links

- [INSTALL.md](INSTALL.md) — Per-OS install and build from source
- [CHANGELOG.md](CHANGELOG.md) — Release history
- [design.md](design.md) — Architecture and design decisions
- [requirements.md](requirements.md) — Functional & non-functional requirements
- [qa_report.md](qa_report.md) — Test results and QA sign-off
