# 21a — Send 响应带回接收方状态面板

Type: Code

## 背景

当前 send 成功后只返回 `{"id": "xxx"}`，task send 失败时只返回 `"target has an in_progress task"`。发送者 agent 得不到任何关于接收方状态的上下文。

## 改动

### 服务端

`pkg/api/handlers.go` — 新增一个状态面板构造逻辑，在 send 和 task send 的响应中带回。

**状态查询：**

handleSend 和 handleSendTask 返回前，查询 Redis：

| 查询 | 来源 |
|------|------|
| target 设备是否存在 | `EXISTS agentlink:device:<device>` |
| target device 的 session | `HGET agentlink:device:<device> sessions` |
| target session 是否有 in_progress task | `SMEMBERS agentlink:tasks:<device>:<session>` → 查 status |
| target 的 inbox 深度 | `LLEN agentlink:inbox:<device>:<session>` |
| target 的 last_seen | `HGET agentlink:device:<device> last_seen` |

**响应格式扩展：**

SendResponse 新增 `recipient_status` 字段：

```go
type RecipientStatus struct {
    Device       string `json:"device,omitempty"`
    Session      string `json:"session,omitempty"`
    Status       string `json:"status"`        // "idle" / "busy" / "offline"
    CurrentTask  string `json:"current_task,omitempty"`   // task_id
    TaskDuration string `json:"task_duration,omitempty"`  // "14m"（已进行时间）
    InboxDepth   int    `json:"inbox_depth,omitempty"`
    LastSeen     string `json:"last_seen,omitempty"`      // 离线时显示
}
```

task send 的 409 响应也改为返回相同结构（而非纯文本错误）。

**面板构造器：**

```go
func (s *Server) buildRecipientStatus(ctx context.Context, device, session string) RecipientStatus
```

### CLI

`pkg/cli/messages.go` — `RunSend` 解析响应中的 recipient_status 并显示：

```
✓ 消息已投递（ID: a1b2c3d4）

超级虫 main session 当前状态:
  忙碌 — 正在处理 task #deploy-042（已进行 14 分钟）
  还有 3 条待读取消息

本消息将在当前任务完成后被读取
```

`pkg/cli/tasks.go` — `RunTaskSend` 在非 200 响应时，尝试解析 `RecipientStatus` 并显示状态面板而非原始错误。

### 不受影响的

- send/task send 失败 / 目标不存在 的场景保持原有流程
- 接收方不在发送者队列里排队等待，系统只告知状态不干预

## Acceptance criteria

- [ ] `send` 成功后响应附带接收方状态面板
- [ ] 接收方忙碌时显示当前 task + 持续时间 + inbox 积压数
- [ ] 接收方空闲时显示 idle
- [ ] 接收方离线时显示最后在线时间
- [ ] `task send` 因忙碌被拦截时返回状态面板（而非纯文本错误）
- [ ] 面板显示友好、agent 可解析（结构体 + 文本同时对齐）
- [ ] 服务端有单元测试覆盖各状态的响应格式
