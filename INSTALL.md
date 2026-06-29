# Installing dsize

`dsize` is a single static binary with no runtime dependencies. Pick the
method that fits your platform.

## Quick install (prebuilt binary)

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/Ridikul/dsize/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/Ridikul/dsize/main/install.ps1 | iex
```

The script picks the right binary for your OS/architecture from the latest
[release](https://github.com/Ridikul/dsize/releases) and puts it on your `PATH`.
Override the location with `BINDIR=~/bin` (or `$env:BINDIR` on Windows).

Then run:

```bash
dsize --ui            # opens the web UI and lets you pick a folder
```

### Gatekeeper / SmartScreen (unsigned binary)

The released binaries are **not code-signed**, so the OS may warn on first run:

- **macOS** — `install.sh` already clears the quarantine flag. If you downloaded
  a binary manually instead, run `xattr -d com.apple.quarantine ./dsize` (or
  right-click → Open the first time).
- **Windows** — SmartScreen may show "Windows protected your PC"; choose
  *More info → Run anyway*.

For signed/notarized distribution you'd need an Apple Developer ID (macOS) or an
Authenticode certificate (Windows) — not required just to try it out.

### Manual download

Grab the binary for your platform from the
[releases page](https://github.com/Ridikul/dsize/releases), `chmod +x` it
(macOS/Linux), and move it onto your `PATH`. Verify integrity against
`checksums.txt`:

```bash
shasum -a 256 -c checksums.txt --ignore-missing
```

## Build from source (all platforms)

Requires **Go 1.22+**.

```bash
cd src
go build -o dsize ./cmd/dsize        # produces ./dsize (dsize.exe on Windows)
```

Then move it onto your `PATH`:

```bash
# macOS (Apple Silicon)
mv dsize /opt/homebrew/bin/

# macOS (Intel) / Linux
sudo mv dsize /usr/local/bin/

# Windows (PowerShell) — move dsize.exe to a folder on %PATH%, e.g.
move dsize.exe %USERPROFILE%\bin\
```

Verify:

```bash
dsize --top 5 .
dsize --ui .          # opens the web UI at http://127.0.0.1:8420
```

## Cross-compile release binaries

Use the bundled `deploy.sh` (from the project root) to produce binaries for
all supported platforms under `dist/`:

```bash
./deploy.sh
ls dist/
# dsize-darwin-arm64  dsize-darwin-amd64  dsize-linux-amd64
# dsize-linux-arm64   dsize-windows-amd64.exe
```

Or cross-compile a single target manually:

```bash
cd src
GOOS=linux  GOARCH=amd64 go build -o dsize-linux-amd64       ./cmd/dsize
GOOS=darwin GOARCH=arm64 go build -o dsize-darwin-arm64      ./cmd/dsize
GOOS=windows GOARCH=amd64 go build -o dsize-windows-amd64.exe ./cmd/dsize
```

Because the web UI assets are embedded with `go:embed`, the cross-compiled
binaries are fully self-contained — no extra files to ship.

## Uninstall

Remove the binary from wherever you installed it, and (optionally) delete the
scan history:

```bash
# macOS
rm -rf "$HOME/Library/Application Support/dsize"
# Linux
rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/dsize"
# Windows (PowerShell)
Remove-Item -Recurse "$env:APPDATA\dsize"
```
