# 24 — Task 回报自动通知发起人

Type: AFK
Blocked by: 无

## 背景

当前 `task result` 成功后只更新 task record(status=completed,result,completed_at),**不通知发起人**。发起人完全不知道任务完成了,只能主动 `task status <id>` 查询。

## 设计

`task result` 和 `task cancel` 成功后,server 端自动往 `issued_by` 的 inbox 里 LPUSH 一条通知。

### 通知格式

```json
{
  "id": "<msg_id>",
  "type": "msg",
  "title": "任务回报 fix-001",
  "from_device": "<worker_device>",
  "from_session": "<worker_session>",
  "content": "completed: 已清理 2.3G",
  "created_at": "<now>"
}
```

注入到发起人 agent 后的显示(和 poller 注入 msg 格式一致):

```
[来自 worker 的消息] 任务回报 fix-001
completed: 已清理 2.3G
```

### server 端改动

`handleTaskResult` — 在更新 task record 后:

1. 解析 `issued_by`(格式: `device:session`)
2. LPUSH 通知到 `agentlink:inbox:<device>:<session>`
3. 设 TTL 7 天

`handleTaskCancel` — 同理,取消时也发通知。

### 不需要手动发 msg

worker 执行完 `task result` 后**不需要额外跑 `agentlink send`**——通知自动投递到发起人 inbox。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | `handleTaskResult` / `handleTaskCancel` 自动发通知 |

## Acceptance criteria

- [ ] worker `task result completed` 后,发起人 inbox 收到一条通知 msg
- [ ] worker `task result suspended` 后,发起人 inbox 收到一条通知 msg
- [ ] sender `task cancel` 后,target inbox 收到一条通知 msg
- [ ] 通知 title = "任务回报 <task_id>"
- [ ] 通知 content = "<status>: <result>"(completed/suspended 时)或 "cancelled"(取消时)
- [ ] 通知不影响 task record 本身的状态
- [ ] `go test ./... -count=1` 全部通过
