#!/bin/sh
# dsize installer for macOS / Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/Ridikul/dsize/main/install.sh | sh
#
# Override the install directory:  BINDIR=~/bin  sh install.sh
# Pin a version:                   DSIZE_VERSION=v2.1.0 sh install.sh
set -e

REPO="Ridikul/dsize"
BINDIR="${BINDIR:-/usr/local/bin}"

os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "dsize: unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *) echo "dsize: unsupported architecture: $arch" >&2; exit 1 ;;
esac
asset="dsize-${os}-${arch}"

tag="${DSIZE_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
fi
[ -n "$tag" ] || { echo "dsize: could not determine the latest release" >&2; exit 1; }

url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
echo "dsize: downloading ${tag} (${os}/${arch})…"
tmp="$(mktemp)"
curl -fSL "$url" -o "$tmp"
chmod +x "$tmp"

# macOS: clear the quarantine flag so Gatekeeper doesn't block the binary.
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$tmp" 2>/dev/null || true
fi

dest="${BINDIR}/dsize"
echo "dsize: installing to ${dest}…"
if [ -w "$BINDIR" ] 2>/dev/null; then
  mv "$tmp" "$dest"
else
  echo "dsize: ${BINDIR} is not writable, using sudo…"
  sudo mv "$tmp" "$dest"
fi

echo "dsize: ✓ installed. Try:  dsize --ui"
