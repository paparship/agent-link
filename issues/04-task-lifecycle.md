# 4 — 任务生命周期

Type: AFK
Blocked by: 2

## Sub-issues

本 issue 拆分为两个独立子 issue，建议按顺序执行：

- [4a — 任务 API 服务端](04a-tasks-api.md)
- [4b — CLI task 子命令](04b-cli-tasks.md)

## What to build

任务的完整生命周期管理，包括发放、执行、挂起、恢复、取消。

服务端：
- `POST /tasks/send`：创建任务（status=issued），写入目标 inbox，返回 msg_id
  - 目标 agent 有 in_progress 任务 → 409
  - 目标 agent 有 ≥ 2 条 suspended 任务 → 409
- `GET /inbox/pull`：拉出 task 类型时，自动将任务状态设为 in_progress
- `POST /tasks/result`：agent 回报，status 可选 `completed` / `suspended`
- `POST /tasks/resume`：main 恢复挂起任务，设为 in_progress，重新写入目标 inbox
- `POST /tasks/cancel`：main 取消任务，设为 cancelled
- `GET /tasks/status?task_id=001`：查询任务状态
- 数据：Redis `task:<task_id>`（status、assigned_to、issued_by、result、suspend_reason 等）

CLI：
- `agentlink task send <target> <task_id> "<content>"`
- `agentlink task result <task_id> <status> "<result>"`
- `agentlink task resume <task_id> "<new_guidance>"`
- `agentlink task cancel <task_id>`
- `agentlink task status <task_id>`

## Acceptance criteria

- [ ] 发任务 → 目标 pull → 自动 in_progress → 回报 completed，状态追踪完整
- [ ] 发任务 → pull → 回报 suspended → 查询状态为 suspended
- [ ] 目标 in_progress 时，再次发任务返回 409
- [ ] 目标 suspended ≥ 2 时，再次发任务返回 409
- [ ] 恢复挂起任务后，目标能再次 pull 到并继续
- [ ] 取消任务后状态为 cancelled
- [ ] content 上限 3000 字符

## Blocked by

- Blocked by #2
