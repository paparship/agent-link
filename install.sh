#!/bin/sh
set -eu

REPO="paparship/agent-link"
BINARY="agentlink"

# --- detect platform ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux)   OS="linux" ;;
  darwin)  OS="darwin" ;;
  *)       echo "unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)           echo "unsupported arch: $ARCH"; exit 1 ;;
esac

# --- download URL ---
# Try latest release first, fall back to tagged version
VERSION="${VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/${BINARY}-${OS}-${ARCH}"
else
  DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/${BINARY}-${OS}-${ARCH}"
fi

# --- detect install dir ---
if [ "$(id -u)" = 0 ]; then
  BINDIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ] && echo ":$PATH:" | grep -q ":${HOME}/.local/bin:"; then
  BINDIR="$HOME/.local/bin"
else
  BINDIR="/usr/local/bin"
fi

# --- download ---
echo "Downloading agentlink for ${OS}-${ARCH}..."
tmpfile="$(mktemp)"
trap 'rm -f "$tmpfile"' EXIT

if ! curl -sfL "$DOWNLOAD_URL" -o "$tmpfile"; then
  echo "error: download failed from $DOWNLOAD_URL"
  exit 1
fi

chmod +x "$tmpfile"

# --- install ---
if [ ! -w "$BINDIR" ]; then
  echo "Installing to $BINDIR requires sudo..."
  sudo mv "$tmpfile" "$BINDIR/$BINARY"
else
  mv "$tmpfile" "$BINDIR/$BINARY"
fi

echo "✓ agentlink installed to $BINDIR/$BINARY"
echo "  Run 'agentlink init --help' to get started"
