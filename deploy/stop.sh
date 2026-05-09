#!/bin/bash

stop_process() {
  local name="$1" pid_file="$2" filter="$3"
  if [ -f "$pid_file" ]; then
    kill $(cat "$pid_file") 2>/dev/null || true
    rm -f "$pid_file"
    echo "$name stopped (via PID file)"
  elif pgrep -f "$filter" > /dev/null 2>&1; then
    pkill -f "$filter" 2>/dev/null || true
    echo "$name stopped (via process name)"
  else
    echo "$name not running"
  fi
}

stop_process "agent-link server" /tmp/agentlink-server.pid "/deploy/server"
stop_process "Redis" /tmp/redis.pid "redis-server /tmp/redis.conf"
