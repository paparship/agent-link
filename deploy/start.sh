#!/bin/bash
set -e

# Start Redis
echo "Starting Redis..."
redis-server /tmp/redis.conf &
echo $! > /tmp/redis.pid
sleep 1

# Verify Redis is up
if ! redis-cli ping 2>/dev/null; then
  echo "Redis failed to start"
  exit 1
fi

# Start agent-link server
echo "Starting agent-link server..."
export REDIS_ADDR=localhost:6379
export REGISTER_PASSWORD="${REGISTER_PASSWORD:-changeme}"
export LISTEN_ADDR=:8080

nohup ./server > server.log 2>&1 &
echo $! > /tmp/agentlink-server.pid
sleep 1

# Verify server is up
if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
  echo "agent-link server started successfully on :8080"
else
  echo "Server failed to start, check server.log"
  exit 1
fi
