# 5 — 心跳 + list

Type: AFK
Blocked by: 2

## What to build

设备在线状态检测和列表展示。

服务端：
- `POST /agents/heartbeat`：更新 `device:<name>` 的 `last_seen` 为当前时间
- `GET /agents/list?all=false`：本设备的 session 列表及在线状态
- `GET /agents/list?all=true`：所有注册设备的 session 列表及在线状态
- 在线判定：`last_seen` 距现在 < 120 秒为在线，否则离线

CLI：
- `agentlink ping` — 手动触发心跳
- 后台每 60 秒自动心跳（在 tmux session 的后台中运行）
- `agentlink list` — 本设备的 session
- `agentlink list --all` — 所有设备

## Acceptance criteria

- [ ] `agentlink ping` 后 `last_seen` 更新
- [ ] `agentlink list` 只显示本设备 session
- [ ] `agentlink list --all` 显示所有注册设备
- [ ] 心跳后 120 秒内显示在线，之后离线
- [ ] 后台自动心跳每 60 秒触发

## Blocked by

- Blocked by #2
