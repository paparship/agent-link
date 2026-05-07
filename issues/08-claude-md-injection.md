# 8 — CLAUDE.md 自动注入

Type: AFK
Blocked by: 2, 3, 4

## What to build

`init`、`session add` 时自动在对应目录的 CLAUDE.md 中追加 agentlink 使用说明。

**main（Router）的 CLAUDE.md 追加内容：**
```
## 通信

- `agentlink task send <target> <task_id> "<content>"` — 发放任务
- `agentlink task resume <task_id> "<guidance>"` — 恢复挂起任务
- `agentlink task cancel <task_id>` — 取消任务
- `agentlink pull` — 拉取回报
- `agentlink list --all` — 查看所有设备状态
```

**worker 的 CLAUDE.md 追加内容：**
```
## 通信

- `agentlink pull` — 拉取任务或消息
- `agentlink task result <task_id> completed "<result>"` — 回报完成
- `agentlink task result <task_id> suspended "<reason>"` — 回报挂起
- `agentlink send <target> "<content>"` — 发送消息
```

**其他 session 类型（如 reviewer）：** `session add` 时根据 session 名字提供对应的说明。

## Acceptance criteria

- [ ] `init` 后 `agent_team/main/CLAUDE.md` 包含 main 的通信说明
- [ ] `init` 后 `agent_team/worker/CLAUDE.md` 包含 worker 的通信说明
- [ ] `session add reviewer` 后 `reviewer/CLAUDE.md` 包含说明
- [ ] 追加时不会删除 CLAUDE.md 原有的内容
- [ ] 内容只含当前 session 需要的命令，无冗余

## Blocked by

- Blocked by #2
- Blocked by #3
- Blocked by #4
