#!/usr/bin/env bash
#
# deploy.sh — cross-compile dsize release binaries into ./dist/
#
# The web UI assets are embedded via go:embed, so every produced binary is
# fully self-contained. Requires Go 1.22+.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$ROOT/src"
DIST="$ROOT/dist"
PKG="./cmd/dsize"

VERSION="${VERSION:-$(date -u +%Y.%m.%d)}"

# platform/arch pairs to build
TARGETS=(
  "darwin/arm64"
  "darwin/amd64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

echo "==> Building dsize $VERSION"
echo "    source: $SRC"
echo "    output: $DIST"

# Fail fast if the code does not build or tests do not pass.
( cd "$SRC" && go vet ./... && go test ./... )

rm -rf "$DIST"
mkdir -p "$DIST"

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  out="dsize-${os}-${arch}"
  [ "$os" = "windows" ] && out="${out}.exe"

  echo "==> $os/$arch -> dist/$out"
  ( cd "$SRC" && \
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" -o "$DIST/$out" "$PKG" )
done

echo
echo "==> Done. Artifacts in $DIST:"
ls -lh "$DIST"
