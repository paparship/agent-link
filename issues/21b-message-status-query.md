# 21b — 消息状态追踪：ID 展示、持久化、message status 查询

Type: Code

## 背景

当前消息没有持久状态。`send` 返回的 ID 在 CLI 中被丢弃，`pull` 的输出也不显示 ID。消息被 `RPop` 消费后不可追溯。发送者无法查询"消息被读了没有"。

## 改动

### 1. Send 显示 ID

`RunSend` 解析响应体中的 `id`，输出时附带：

```
✓ 消息已投递（ID: a1b2c3d4）
```

### 2. Pull 显示 ID

`RunPull` 的输出格式增加消息 ID：

```
[msg] a1b2c3d4 from jiefan-local:main — 2026-05-10T03:40Z
  看看日志
```

### 3. 消息持久化

当前：`LPUSH` 到 inbox list，`RPop` 消费即删。

改为：
- inbox 仍用 `LPUSH` 作为投递队列（保持不变）
- 发送时额外写一条独立消息记录：`agentlink:msg:<msg_id>` (Hash)，字段：

| 字段 | 值 |
|------|-----|
| `from_device` | 发送方设备 |
| `from_session` | 发送方 session |
| `to_device` | 目标设备 |
| `to_session` | 目标 session |
| `content` | 消息内容 |
| `type` | msg / task |
| `task_id` | task 时可选 |
| `sent_at` | 发送时间戳 |
| `delivered_at` | 拉取时间戳（空表示未读） |

- 消息记录带 TTL：已读取后 24h 过期，未读取 7 天过期
- `RPop` 时回填 `delivered_at`

### 4. Message status 命令

新增 CLI 命令 `agentlink message status <id>`：

```
agentlink message status a1b2c3d4
→ 消息 a1b2c3d4
  发送: jiefan-local:main → supermicro:main
  内容: 看看日志
  时间: 2026-05-10 03:40Z
  
  状态: 已读取（5 分钟前）

  或: 状态: 待读取（inbox 中）
  或: 状态: 未投递（接收方已离线，消息将在上线后投递）
  或: 状态: 已过期（消息已超过 7 天）
```

服务端新增 `GET /messages/status?id=<msg_id>` 端点，查询 `agentlink:msg:<msg_id>` 记录。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | 消息发送时写 msg record + 新增 handleMsgStatus |
| `pkg/api/server.go` | 注册 GET /messages/status |
| `pkg/cli/messages.go` | RunSend 解析 ID 并展示 |
| `pkg/cli/messages.go` | RunPull 展示 msg ID |
| `cmd/agentlink/main.go` | 注册 `message status` 子命令 |

## 不做

- 不保存完整的消息历史记录（只保留 TTL 内可查）
- 不实现消息搜索或批量查询
- 不实现"已读回执推送"（发送者主动查询而非被动通知）

## Acceptance criteria

- [ ] `send` 输出显示消息 ID
- [ ] `pull` 输出显示消息 ID
- [ ] 服务端写 `agentlink:msg:<id>` 记录（带 TTL）
- [ ] 消息被 `RPop` 后回填 `delivered_at`
- [ ] 已读消息 24h 后自动过期
- [ ] 未读消息 7 天后自动过期
- [ ] `message status <id>` 可查已读/未读/过期状态
- [ ] `message status <id>` 不存在的 ID 友好提示
- [ ] `go test ./... -count=1` 全部通过
