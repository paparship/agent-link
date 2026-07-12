# 25 — Task 重新发布(Re-open)

Type: AFK
Blocked by: 无

## 背景

当前状态机 completed/cancelled 是终态,无法回到可执行状态。但实际场景中有重新打开的需求:

- 任务因客观条件不满足被 cancel,条件恢复后想重发
- 任务被误判 completed,实际还有后续工作
- 挂起的任务需要换一个 worker 重新发

每次重新打开都要手动重新写 task_id + content,和原 task 失去关联。

## 设计

新增 `agentlink task reopen <task_id>` 命令。server 端把 task 状态重置为 `issued`,重新推进目标 inbox。

### 状态检查

只有 `completed` 或 `cancelled` 的任务可以 reopen。`in_progress` 的任务用 `task resume`,不在这里。

```
completed ──→ (reopen) ──→ issued ──→ in_progress → ...
cancelled ──→ (reopen) ──→ issued ──→ in_progress → ...
```

### server 端改动

`POST /tasks/reopen`:

1. 查 task 存在,状态是 completed 或 cancelled,否则拒绝
2. 把 task status 改为 `issued`,清除 `completed_at`/`result`
3. 把 task_id 重新 SAdd 到 `agentlink:tasks:<assigned_to>`
4. LPUSH 到目标 inbox

### CLI 改动

```bash
agentlink task reopen <task_id>
```

task_id 复用原来的,content/title/assigned_to 不变。

## 不做

- 不换目标 device/session(reopen 后原封不动发给原来的 assigned_to)
- 不修改 content/title(要改就是新 task 了)

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | 新增 `handleTaskReopen` |
| `pkg/api/server.go` | 注册 `POST /tasks/reopen` |
| `pkg/cli/net/tasks.go` | 新增 `RunTaskReopen` |
| `cmd/agentlink/main.go` | 注册 `task reopen` 子命令 + printUsage |

## Acceptance criteria

- [ ] `task reopen <id>` 把 completed task 重置为 issued,重新进 inbox
- [ ] `task reopen <id>` 把 cancelled task 重置为 issued,重新进 inbox
- [ ] `task reopen <id>` 对 in_progress task 返回错误(用 task resume)
- [ ] `task reopen <id>` 对 suspended task 返回错误(用 task resume)
- [ ] `task reopen <id>` 对不存在的 task 返回 404
- [ ] reopen 后 task record 的 `result`/`completed_at` 被清空
- [ ] `go test ./... -count=1` 全部通过
