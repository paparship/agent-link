#!/bin/bash
# agentlink API integration tests
# Usage: SERVER=http://YOUR_SERVER_IP:8080 PASSWORD=YOUR_REGISTER_PASSWORD bash api_test.sh

set -uo pipefail

SERVER="${SERVER:-http://YOUR_SERVER_IP:8080}"
PASSWORD="${PASSWORD:-YOUR_REGISTER_PASSWORD}"
PASS=0
FAIL=0
DEVICE="test-int-$$"  # unique per run
API_KEY=""

cleanup() {
  [[ -n "$API_KEY" ]] && curl -sf -X DELETE "$SERVER/agents/device" \
    -H "Authorization: Bearer $API_KEY" > /dev/null 2>&1 || true
  # Clean up Redis test data
  ssh YOUR_SERVER_IP "redis-cli KEYS 'agentlink:*'" 2>/dev/null | \
    grep "test-int" | xargs -r redis-cli DEL > /dev/null 2>&1 || true
}

check() {
  local name="$1" status="$2"
  if [[ "$status" -eq 0 ]]; then
    echo "  PASS  $name"
    ((PASS++))
  else
    echo "  FAIL  $name"
    ((FAIL++))
  fi
}

do_api() {
  local method="$1" path="$2" body="$3" expected="${4:-200}"
  local auth="${5:-}"
  local headers=(-H "Content-Type: application/json")
  [[ -n "$auth" ]] && headers+=(-H "Authorization: Bearer $auth")

  if [[ -n "$body" ]]; then
    curl -s -o /tmp/al-resp.json -w "%{http_code}" -X "$method" \
      "$SERVER$path" "${headers[@]}" -d "$body"
  else
    curl -s -o /tmp/al-resp.json -w "%{http_code}" -X "$method" \
      "$SERVER$path" "${headers[@]}"
  fi
}

extract() { python3 -c "import json; j=json.load(open('/tmp/al-resp.json')); print($1)" 2>/dev/null; }

# ───────────────────────────────

echo "=========================================="
echo " agentlink API Integration Tests"
echo " Server: $SERVER"
echo " Device: $DEVICE"
echo "=========================================="

# 1. Health
echo ""
echo "=== Health ==="
code=$(do_api GET /health "")
if [[ "$code" == "200" ]] && [[ "$(extract "j['ok']")" == "True" ]]; then
  check "health check returned ok" 0
else
  check "health check (got $code)" 1
fi

# 2. Register
echo ""
echo "=== Register ==="
code=$(do_api POST /agents/register \
  "{\"device\":\"$DEVICE\",\"sessions\":[\"main\"],\"register_password\":\"$PASSWORD\"}")
API_KEY=$(extract "j['api_key']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ -n "$API_KEY" ]]; then
  check "device registered, api_key received" 0
else
  check "register (got $code)" 1
fi

# 3. Auth — no key → 401
echo ""
echo "=== Auth ==="
code=$(do_api GET /agents/list "" "" "")
if [[ "$code" == "401" ]]; then
  check "unauthorized request returns 401" 0
else
  check "unauthorized (got $code)" 1
fi

# 4. Bad auth → 401
code=$(do_api GET "/agents/list" "" "" "invalid_key")
if [[ "$code" == "401" ]]; then
  check "bad api key returns 401" 0
else
  check "bad key (got $code)" 1
fi

# 5. Send message
echo ""
echo "=== Send Message ==="
code=$(do_api POST /messages/send \
  "{\"to\":\"$DEVICE:main\",\"from_session\":\"main\",\"content\":\"hello from test\"}" "" "$API_KEY")
MSG_ID=$(extract "j['id']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ -n "$MSG_ID" ]]; then
  check "message sent, got id: ${MSG_ID::8}" 0
else
  check "send message (got $code)" 1
fi

# 6. Pull message
echo ""
echo "=== Pull ==="
code=$(do_api GET "/inbox/pull?session=main&limit=1" "" "" "$API_KEY")
PULLED_ID=$(extract "j['items'][0]['id']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ "$PULLED_ID" == "$MSG_ID" ]]; then
  check "message pulled, id matches" 0
else
  check "pull (got $code, pulled=$PULLED_ID)" 1
fi

# 7. Empty inbox
code=$(do_api GET "/inbox/pull?session=main" "" "" "$API_KEY")
ITEM_COUNT=$(extract "len(j['items'])" 2>/dev/null || echo "1")
if [[ "$code" == "200" ]] && [[ "$ITEM_COUNT" -eq 0 ]]; then
  check "empty inbox after pull" 0
else
  check "empty inbox (got $code, items=$ITEM_COUNT)" 1
fi

# 8. Message query — should show delivered
echo ""
echo "=== Message Status ==="
code=$(do_api GET "/messages/status?id=$MSG_ID" "" "" "$API_KEY")
STATUS=$(extract "j['status']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ "$STATUS" == "delivered" ]]; then
  check "message status delivered" 0
else
  check "msg status (got $code, status=$STATUS)" 1
fi

# 9. Message not found
code=$(do_api GET "/messages/status?id=nonexistent00001" "" "" "$API_KEY")
if [[ "$code" == "404" ]]; then
  check "non-existent message returns 404" 0
else
  check "not found (got $code)" 1
fi

# 10. Send task
echo ""
echo "=== Task ==="
code=$(do_api POST /tasks/send \
  "{\"to\":\"$DEVICE:main\",\"from_session\":\"main\",\"task_id\":\"int-$$-task\",\"content\":\"test task\"}" "" "$API_KEY")
SENT_TASK_ID=$(extract "j['task_id']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ -n "$SENT_TASK_ID" ]]; then
  check "task sent, task_id=$SENT_TASK_ID" 0
else
  check "task send (got $code)" 1
fi

# 11. Task status (should be issued)
code=$(do_api GET "/tasks/status?task_id=$SENT_TASK_ID" "" "" "$API_KEY")
TASK_STATUS=$(extract "j['status']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ "$TASK_STATUS" == "issued" ]]; then
  check "task status: issued" 0
else
  check "task status (got $code, status=$TASK_STATUS)" 1
fi

# 12. Pull task → in_progress
code=$(do_api GET "/inbox/pull?session=main&limit=1" "" "" "$API_KEY")
code=$(do_api GET "/tasks/status?task_id=$SENT_TASK_ID" "" "" "$API_KEY")
TASK_STATUS=$(extract "j['status']" 2>/dev/null || echo "")
if [[ "$code" == "200" ]] && [[ "$TASK_STATUS" == "in_progress" ]]; then
  check "after pull, task status: in_progress" 0
else
  check "task in_progress (got $code, status=$TASK_STATUS)" 1
fi

# 13. Busy check — try to send another task while in_progress
code=$(do_api POST /tasks/send \
  "{\"to\":\"$DEVICE:main\",\"from_session\":\"main\",\"task_id\":\"int-$$-blocked\",\"content\":\"should block\"}" "" "$API_KEY")
BUSY_STATUS=$(extract "j['recipient_status']['status']" 2>/dev/null || echo "")
if [[ "$code" == "409" ]] && [[ "$BUSY_STATUS" == "busy" ]]; then
  check "409 busy + recipient_status panel" 0
else
  check "busy check (got $code, status=$BUSY_STATUS)" 1
fi

# 14. Complete task
code=$(do_api POST /tasks/result \
  "{\"task_id\":\"$SENT_TASK_ID\",\"status\":\"completed\",\"result\":\"done\"}" "" "$API_KEY")
if [[ "$code" == "200" ]]; then
  check "task completed" 0
else
  check "task result (got $code)" 1
fi

# 15. Cancel another task (send + cancel)
code=$(do_api POST /tasks/send \
  "{\"to\":\"$DEVICE:main\",\"from_session\":\"main\",\"task_id\":\"int-$$-cancel\",\"content\":\"to cancel\"}" "" "$API_KEY")
code=$(do_api POST /tasks/cancel \
  "{\"task_id\":\"int-$$-cancel\"}" "" "$API_KEY")
if [[ "$code" == "200" ]]; then
  check "task cancelled" 0
else
  check "task cancel (got $code)" 1
fi

# 16. Task list — should be empty (both tasks completed/cancelled)
code=$(do_api GET "/tasks/list?session=main" "" "" "$API_KEY")
TASK_COUNT=$(extract "len(j['tasks'])" 2>/dev/null || echo "0")
if [[ "$code" == "200" ]] && [[ "$TASK_COUNT" -eq 0 ]]; then
  check "no active tasks after cleanup" 0
else
  check "task list (got $code, count=$TASK_COUNT)" 1
fi

# 17. Heartbeat
echo ""
echo "=== Heartbeat ==="
code=$(do_api POST /agents/heartbeat "" "" "$API_KEY")
if [[ "$code" == "200" ]]; then
  check "heartbeat sent" 0
else
  check "heartbeat (got $code)" 1
fi

# 18. List devices
echo ""
echo "=== List ==="
code=$(do_api GET "/agents/list?all=true" "" "" "$API_KEY")
DEVICE_COUNT=$(extract "len(j['agents'])" 2>/dev/null || echo "0")
if [[ "$code" == "200" ]] && [[ "$DEVICE_COUNT" -ge 1 ]]; then
  check "device list returned $DEVICE_COUNT devices" 0
else
  check "list (got $code, count=$DEVICE_COUNT)" 1
fi

# 19. Invalid inputs
echo ""
echo "=== Input Validation ==="
code=$(do_api POST /messages/send \
  "{\"to\":\"$DEVICE:main\",\"from_session\":\"main\",\"content\":\"\"}" "" "$API_KEY")
if [[ "$code" == "400" ]]; then
  check "empty content returns 400" 0
else
  check "empty content (got $code)" 1
fi

code=$(do_api POST /messages/send \
  "{\"to\":\"nonexist:main\",\"from_session\":\"main\",\"content\":\"hi\"}" "" "$API_KEY")
if [[ "$code" == "404" ]]; then
  check "invalid target returns 404" 0
else
  check "invalid target (got $code)" 1
fi

# 20. Cleanup — delete device
echo ""
echo "=== Cleanup ==="
code=$(do_api DELETE /agents/device "" "" "$API_KEY")
if [[ "$code" == "200" ]]; then
  check "device deleted" 0
else
  check "delete (got $code)" 1
fi

# Verify device is gone
code=$(do_api GET "/agents/list?all=true" "" "" "$API_KEY")
AGENT_NAMES=$(extract "j['agents']" 2>/dev/null || echo "")
if [[ "$AGENT_NAMES" != *"$DEVICE"* ]]; then
  check "device removed from listing" 0
else
  check "device removal verification" 1
fi

# ───────────────────────────────
echo ""
echo "=========================================="
echo " Results: $PASS passed / $FAIL failed"
echo "=========================================="

# Clean up test device if still exists
cleanup

[[ "$FAIL" -eq 0 ]]
