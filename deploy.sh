#!/bin/bash
# Full reinstall: build, deploy, uninstall, re-init both machines
set -e

SERVER=YOUR_SERVER_IP
PASSWORD=YOUR_REGISTER_PASSWORD
LOCAL_DEVICE=jiefan-local
REMOTE_DEVICE=YOUR_HOSTNAME
REMOTE_BIN=/home/jiefan/agent-link/deploy/agentlink
REMOTE_PROJECT=/home/jiefan/agentlink-server
LOCAL_PROJECT=~/agentlink-test

echo "=== 1. Build ==="
cd "$(dirname "$0")"
go build -o agentlink ./cmd/agentlink/

echo "=== 2. Deploy binaries ==="
cp agentlink ~/.local/bin/agentlink.new
mv ~/.local/bin/agentlink.new ~/.local/bin/agentlink

scp agentlink "$SERVER:$REMOTE_BIN.new"
ssh "$SERVER" "mv $REMOTE_BIN.new $REMOTE_BIN"
ssh "$SERVER" "cp $REMOTE_BIN ~/.local/bin/agentlink.new && mv ~/.local/bin/agentlink.new ~/.local/bin/agentlink"

echo "=== 3. Uninstall (local + remote) ==="
agentlink uninstall --purge 2>/dev/null || true
rm -rf "$LOCAL_PROJECT"
ssh "$SERVER" "$REMOTE_BIN uninstall --purge 2>/dev/null; rm -rf $REMOTE_PROJECT" || true

echo "=== 4. Init ==="
agentlink init --server http://$SERVER:8080 --password "$PASSWORD" --device "$LOCAL_DEVICE" "$LOCAL_PROJECT" 2>&1 | head -5
ssh "$SERVER" "$REMOTE_BIN init --server http://localhost:8080 --password '$PASSWORD' --device '$REMOTE_DEVICE' '$REMOTE_PROJECT'" 2>&1 | head -5

echo "=== 5. Wait for heartbeat ==="
sleep 10
cd "$LOCAL_PROJECT/main"
agentlink list --all
