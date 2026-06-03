#!/bin/bash
# agentlink end-to-end CLI tests (dual-device)
# Simulates real user behavior: send, check status, manage tasks.
# No manual `agentlink pull` — poller handles that automatically.
# Requires: deploy.sh already run, both devices online
# Usage: bash cli_test.sh

set -uo pipefail

LOCAL_DEVICE="${LOCAL_DEVICE:-jiefan-local}"
REMOTE_DEVICE="${REMOTE_DEVICE:-vm-0-9-opencloudos}"
REMOTE_HOST="${REMOTE_HOST:-101.34.212.20}"
REMOTE_BIN="${REMOTE_BIN:-/home/jiefan/agent-link/deploy/agentlink}"
LOCAL_DIR="${LOCAL_DIR:-$HOME/agentlink-test/main}"
PASS=0
FAIL=0

GR="\033[32m" RD="\033[31m" RS="\033[0m"

check() {
  local name="$1" status="$2"
  if [[ "$status" -eq 0 ]]; then
    echo -e "  ${GR}PASS${RS}  $name"; ((PASS++))
  else
    echo -e "  ${RD}FAIL${RS}  $name"; ((FAIL++))
  fi
}

local_al() { (cd "$LOCAL_DIR" && agentlink "$@" 2>&1); }
remote_al() { ssh "$REMOTE_HOST" "cd ~/agentlink-server/main && $REMOTE_BIN $*" 2>&1; }

wait_status() {
  local id="$1" target="$2" n=0
  while [[ $n -lt 8 ]]; do
    out=$(local_al message status "$id" 2>&1)
    if echo "$out" | grep -q "$target"; then echo "$out"; return 0; fi
    sleep 1; ((n++))
  done
  echo "$out"; return 1
}

wait_task() {
  local tid="$1" target="$2" n=0
  while [[ $n -lt 8 ]]; do
    out=$(local_al task status "$tid" 2>&1)
    if echo "$out" | grep -q "$target"; then echo "$out"; return 0; fi
    sleep 1; ((n++))
  done
  echo "$out"; return 1
}

cleanup() {
  local_al task cancel cli-e2e-msg-$$ 2>/dev/null || true
  local_al task cancel cli-e2e-tsk-$$ 2>/dev/null || true
  local_al task cancel cli-e2e-busy-$$ 2>/dev/null || true
}

echo "=========================================="
echo " agentlink End-to-End CLI Tests"
echo " Local:  $LOCAL_DEVICE"
echo " Remote: $REMOTE_DEVICE"
echo " (poller auto-injects, never manual pull)"
echo "=========================================="

# ═══════════════════════════════════════════════
echo ""
echo "=== Basics ==="

out=$(local_al ping 2>&1)
echo "$out" | grep -q "Heartbeat sent" && check "ping (local)" 0 || check "ping (local)" 1

out=$(remote_al ping 2>&1)
echo "$out" | grep -q "Heartbeat sent" && check "ping (remote)" 0 || check "ping (remote)" 1

out=$(local_al list --all 2>&1)
if echo "$out" | grep -q "$LOCAL_DEVICE" && echo "$out" | grep -q "$REMOTE_DEVICE"; then
  check "list --all shows both devices" 0
else
  check "list --all" 1
fi

# ═══════════════════════════════════════════════
echo ""
echo "=== Cross-Device Message ==="

MSG_TEXT="e2e-test-$(date +%H%M%S)"
out=$(local_al send "$REMOTE_DEVICE:main" "$MSG_TEXT" 2>&1)
MSG_ID=$(echo "$out" | grep -oP 'ID: \K[a-f0-9]+' || echo "")
[[ -n "$MSG_ID" ]] && check "send local → remote (ID: ${MSG_ID::8})" 0 || check "send local → remote" 1

# Wait until poller delivers it (max 8s)
out=$(wait_status "$MSG_ID" "delivered")
echo "$out" | grep -q "delivered" && check "message auto-delivered by poller" 0 || check "message delivered (got: pending)" 1

# Check status panel on send (remote should be idle)
out=$(local_al send "$REMOTE_DEVICE:main" "status check" 2>&1)
echo "$out" | grep -q "空闲" && check "status panel: idle" 0 || check "status panel: idle" 1

# ═══════════════════════════════════════════════
echo ""
echo "=== Reverse: Remote → Local ==="

out=$(remote_al send "$LOCAL_DEVICE:main" "hello from remote" 2>&1)
echo "$out" | grep -q "已投递" && check "remote send → local" 0 || check "remote send → local" 1

# ═══════════════════════════════════════════════
echo ""
echo "=== Task Lifecycle ==="

# Create task
out=$(local_al task send "$REMOTE_DEVICE:main" cli-e2e-msg-$$ "e2e task: process this" 2>&1)
echo "$out" | grep -q "sent to" && check "task send (local → remote)" 0 || check "task send" 1

# Task becomes in_progress (poller delivers it)
out=$(wait_task "cli-e2e-msg-$$" "in_progress")
echo "$out" | grep -q "in_progress" && check "task auto-injected, status: in_progress" 0 || check "task in_progress (got: $out)" 1

# Complete on remote side
out=$(remote_al task result cli-e2e-msg-$$ completed "e2e passed" 2>&1)
echo "$out" | grep -q "completed" && check "remote reports completed" 0 || check "remote task result" 1

# Verify completion
out=$(wait_task "cli-e2e-msg-$$" "completed")
echo "$out" | grep -q "completed" && check "task status: completed" 0 || check "task completed (got: $out)" 1

# Cancel a separate task
out=$(local_al task send "$REMOTE_DEVICE:main" cli-e2e-tsk-$$ "to be cancelled" 2>&1)
wait_task "cli-e2e-tsk-$$" "in_progress" > /dev/null 2>&1
out=$(local_al task cancel cli-e2e-tsk-$$ 2>&1)
echo "$out" | grep -q "cancelled" && check "task cancel" 0 || check "task cancel" 1

out=$(local_al task list 2>&1)
echo "$out" | grep -q "No active tasks" && check "task list: no active" 0 || check "task list" 1

# ═══════════════════════════════════════════════
echo ""
echo "=== Busy Detection ==="

# Make remote busy
out=$(local_al task send "$REMOTE_DEVICE:main" cli-e2e-busy-$$ "keep remote busy" 2>&1)
wait_task "cli-e2e-busy-$$" "in_progress" > /dev/null 2>&1

# Send while remote is busy → should show busy
out=$(local_al send "$REMOTE_DEVICE:main" "are you busy?" 2>&1)
echo "$out" | grep -q "忙碌" && check "status panel: busy when remote has in_progress task" 0 || check "status panel: busy (got: idle)" 1

# Cleanup
remote_al task cancel cli-e2e-busy-$$ > /dev/null 2>&1 || true
local_al task cancel cli-e2e-busy-$$ > /dev/null 2>&1 || true

# Verify back to idle
sleep 2
out=$(local_al send "$REMOTE_DEVICE:main" "idle check" 2>&1)
echo "$out" | grep -q "空闲" && check "status panel: idle after cleanup" 0 || check "status panel: idle after cleanup" 1

# ═══════════════════════════════════════════════
echo ""
echo "=== Error Handling ==="

out=$(local_al send 2>&1 || true)
echo "$out" | grep -qi "usage" && check "send without args shows usage" 0 || check "send without args" 1

out=$(local_al task send 2>&1 || true)
echo "$out" | grep -qi "usage" && check "task send without args shows usage" 0 || check "task send without args" 1

out=$(local_al send "no-such-dev:main" "test" 2>&1 || true)
echo "$out" | grep -qi "not found" && check "send to non-existent device" 0 || check "non-existent device" 1

out=$(local_al message status "00000000000000000000000000000000" 2>&1 || true)
echo "$out" | grep -qi "not found" && check "message status for bad ID" 0 || check "bad message id" 1

# ═══════════════════════════════════════════════
cleanup

echo ""
echo "=========================================="
echo -e " Results: ${GR}$PASS passed${RS} / ${RD}$FAIL failed${RS}"
echo "=========================================="

[[ "$FAIL" -eq 0 ]]
