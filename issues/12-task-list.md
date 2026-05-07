# 12 — Task 流程闭环：自动 ID + 注入 + 列表

Type: AFK

## What to build

修复 task 流程中的三个缺口，让 Worker Agent 能完整地接收→执行→回报任务。

**缺口回顾：**

```
Main 发任务 → 收件箱 → Poller 注入 → Claude Code 看到 "查询配置"
                                          ↑ 没有 task_id，不知道用什么回报
Puller 拉取 → 看到 "[task] 查询配置"
                ↑ 也没有 task_id
```

**三个改动：**

### 1. task_id 自动生成（服务端 + CLI）

**服务端 `pkg/api/handlers.go`：**
- `SendTaskRequest.TaskID` 改为可选（`json:"task_id,omitempty"`）
- 空时自动生成 8 位随机 hex（如 `a3f8c9e1`）
- 响应返回生成的 task_id

**CLI `cmd/agentlink/main.go`：**
- `agentlink task send <target> "<content>"` — 不填 ID，让服务端生成
- 保留 3 参数形式 `agentlink task send <target> <id> "<content>"` 用于需要指定 ID 的场景

### 2. Poller 注入时带 task_id

**`pkg/cli/poller.go`：**
- `pollerInboxItem` 加 `TaskID` 字段
- 注入格式改为：`[Task from X:Y] (task_id) 内容`
- 这样 Claude Code 能看到 task_id，知道用哪个 ID 回报

### 3. Pull 展示 task_id

**`pkg/cli/messages.go`：**
- `RunPull` 解析响应中的 `task_id`
- task 类型展示：`Task ID: a3f8c9e1`

### 4. task list 命令

**服务端 `pkg/api/handlers.go`：**
- 新增 `GET /tasks/list?session=<name>` 端点
- `SMEMBERS tasks:<device>:<session>` 获取活跃任务 ID
- 返回每个任务的 status / content / assigned_to / issued_by / issued_at

**CLI `pkg/cli/tasks.go`：**
- 新增 `RunTaskList()`
- `agentlink task list` 列出当前 session 下的活跃任务

## Motivation

Worker 收到 task 后看不到 task_id，无法用 `task result` 回报。整个 task 流程因此断裂。

## Acceptance criteria

- [ ] `agentlink task send worker "查询配置"` — 不填 ID，成功返回生成的 ID
- [ ] `agentlink task send worker my-id "查询配置"` — 仍支持手动指定 ID
- [ ] Poller 注入时 content 中带 `task_id`（Claude Code 能看到）
- [ ] `agentlink pull` 展示 `Task ID: xxx`
- [ ] `agentlink task list` 列出当前 session 的活跃任务
- [ ] 无任务时显示 "No active tasks"
- [ ] `go test ./... -count=1` 全部通过
- [ ] 两端（server + agentlink）重新部署

## Blocked by

None — 可以立即开始
