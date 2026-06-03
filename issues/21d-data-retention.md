# 21d — 数据保留策略（TTL + 配额）

Type: AFK

## 背景

当前 agentlink 在 Redis 中的数据没有 TTL 或保留策略：
- inbox 消息无限堆积（除非设备注销）
- task 记录永久保留
- 设备心跳无过期

随着 21b 引入消息持久化后，数据量会进一步增长，必须有清理机制。

## 保留策略

### 消息（inbox）

| 类型 | 策略 | 实现 |
|------|------|------|
| 未读取 | TTL 7 天 | `EXPIRE agentlink:inbox:<d>:<s> 604800` |
| 已读取后的状态记录 | TTL 24 小时 | `EXPIRE agentlink:msg:<id> 86400` |
| 中断标记消息 | 不特殊处理 | 跟随 inbox TTL |

### Task

| 类型 | 策略 |
|------|------|
| 活跃任务（issued / in_progress / suspended） | 不设 TTL |
| 已完成/已取消 | TTL 30 天 |

### 设备

| 数据 | 策略 |
|------|------|
| device 记录 | 不设 TTL（除非注销） |
| 心跳 last_seen | 每次心跳更新，无独立 TTL |
| 离线检测 | 心跳超过 5 分钟视为离线 |

## 改动

### 服务端

`pkg/api/handlers.go` — 在各写入操作后设置 `EXPIRE`：

- `handleSend` LPUSH 后设 inbox TTL
- `handleSendTask` LPUSH 后设 inbox TTL
- `handleTaskResult` / `handleTaskCancel` 完成后设 task TTL
- `handleMsgStatus` 新增的 msg record 设 TTL

`pkg/api/server.go` — 可选：启动一个后台 GC goroutine，定期清理孤儿 key（不被任何 inbox 引用的 msg record）。

## 配置

可选：TTL 通过环境变量或 config 配置（先做固定值，不做可配置）。

## Acceptance criteria

- [ ] 未读取消息 7 天后自动过期
- [ ] 已读取消息状态记录 24h 后过期
- [ ] 已完成/已取消 task 30 天后过期
- [ ] 活跃 task 不受 TTL 影响
- [ ] inbox 过期不影响独立 msg record 的 TTL
- [ ] 设备数据不受 TTL 影响
