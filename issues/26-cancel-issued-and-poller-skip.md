# 26 — Cancel issued task + poller 跳过非 issued task

Type: AFK
Blocked by: 无

## 背景

两个关联缺口:

1. `task cancel` 只接受 `in_progress` 状态,不支持取消 `issued` 状态的任务(还在 inbox 里排队的)。发送方发完后悔了,无法撤回。
2. poller(和手动 `pull`) 取到 inbox 里的 task 时,不检查 task record 状态。如果任务已被 cancel,agent 仍然看到注入内容,然后才发现查 task 状态是 cancelled——浪费注意力。

## 设计

### handleTaskCancel 扩展

支持 `issued` 和 `suspended` 状态的取消,不只是 `in_progress`:

```
issued    → cancelled  (从 inbox 队列中移除 in-memory 不可行,靠 pull 端过滤)
in_progress → cancelled  (现有逻辑,加通知到 target inbox)
suspended → cancelled  (新增)
completed → 拒绝 (已完结,不可再取消)
cancelled → 拒绝 (幂等)
```

取消时:
1. 置 status = `cancelled`,设 `completed_at`
2. SRem 从跟踪集合
3. 如果是 `in_progress`,LPUSH 一条 cancel 通知到 target inbox(告知 agent 当前工作被取消)

### handlePull 过滤

取到 inbox item 后,如果 `Type == "task"` 且 `TaskID != ""`:

```
查 task record status:
  status == "issued" → 正常注入 + 置 in_progress
  status != "issued" → 跳过(已 cancel/suspend/completed),不注入
```

这条过滤对 poller 和手动 `pull` 同时生效。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | `handleTaskCancel` 扩展状态 + `handlePull` 加过滤 |

## Acceptance criteria

- [ ] `task cancel` 对 `issued` 任务成功,cancel 后 poller pull 不注入
- [ ] `task cancel` 对 `suspended` 任务成功
- [ ] `task cancel` 对 `completed` 任务返回 400
- [ ] `task cancel` 对 `cancelled` 任务返回 400(幂等)
- [ ] cancel `in_progress` 任务时往 target inbox 推一条 cancel 通知
- [ ] `handlePull` 跳过非 issued 的 task,msg 不受影响
- [ ] `agentlink pull --all` 看不到已 cancel 的 task
- [ ] `go test ./... -count=1` 全部通过
