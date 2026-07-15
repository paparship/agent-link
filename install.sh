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

# --- check download tool ---
if command -v curl >/dev/null 2>&1; then
  DL_CMD="curl -sfL"
elif command -v wget >/dev/null 2>&1; then
  DL_CMD="wget -qO-"
else
  echo "error: need curl or wget to download"
  exit 1
fi

# --- download ---
echo "Downloading agentlink for ${OS}-${ARCH}..."
tmpfile="$(mktemp /tmp/agentlink.XXXXXXXX)"
trap 'rm -f "$tmpfile"' EXIT

if ! $DL_CMD "$DOWNLOAD_URL" > "$tmpfile"; then
  echo "error: download failed (check network connectivity and URL)"
  echo "  $DOWNLOAD_URL"
  exit 1
fi

chmod +x "$tmpfile"

# --- install ---
if [ ! -w "$BINDIR" ]; then
  echo "Installing to $BINDIR requires sudo..."
  if ! sudo -n true 2>/dev/null; then
    echo "  sudo access required. You can also install manually:"
    echo "  cp $tmpfile $BINDIR/$BINARY"
    exit 1
  fi
  sudo mv "$tmpfile" "$BINDIR/$BINARY"
else
  mv "$tmpfile" "$BINDIR/$BINARY"
fi

# --- ensure tmux ---
TMUX_MISSING=0
command -v tmux >/dev/null 2>&1 || TMUX_MISSING=1

echo "✓ agentlink installed to $BINDIR/$BINARY"
installed_version="$("$BINDIR/$BINARY" version 2>/dev/null | awk '{print $2}')"
echo "  version: ${installed_version:-unknown}"
if [ "$TMUX_MISSING" -eq 1 ]; then
  echo ""
  echo "  ⚠ tmux is required. Install it:"
  case "$OS" in
    linux)
      echo "    apt install -y tmux    (Debian/Ubuntu)"
      echo "    yum install -y tmux    (RHEL/CentOS)"
      ;;
    darwin)
      echo "    brew install tmux"
      ;;
  esac
fi
echo "  Run 'agentlink init --help' to get started"
