#!/bin/sh
set -eu

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${1:-$REPO_DIR/server}"
SERVICE="agent-link-server"

if [ ! -f "$BINARY" ]; then
  echo "usage: $0 [path/to/server]"
  echo "error: binary not found at $BINARY"
  exit 1
fi

echo "→ Stopping $SERVICE..."
sudo systemctl stop "$SERVICE"

echo "→ Installing $BINARY..."
sudo cp "$BINARY" /home/jiefan/agent-link/deploy/server

echo "→ Starting $SERVICE..."
sudo systemctl start "$SERVICE"

sleep 1

echo "→ Health check..."
curl -sf http://localhost:8080/health | python3 -c "import json,sys; d=json.load(sys.stdin); print('✓', d.get('redis', 'unknown')) if d.get('ok') else print('✗', d)"

echo "✓ Done"
