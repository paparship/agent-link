# 27 — Agent 自我身份恢复

Type: AFK
Blocked by: 无

## 目标

agent 在 context 压缩后遗忘身份时,能自主找回"我是谁、我在做什么、我有什么活、我该怎么操作"。

四个改动服务于同一个目的:

## 子 issue

### 27a — task list 收到/发出 + issued 反向索引

`task list` 当前只显示"我收到的 task"。加上 issued 反向索引,同时显示"我发出的 task"。

- task send 时新增 `SADD agentlink:issued:<sender>:<session> <task_id>`
- task completed/cancelled 时同步 SRem
- `handleTaskList` 改查两个集合(received + sent)
- CLI 输出分 Received/Sent 两组

### 27b — current 字段带 task_id

`current` 字段当前格式:`task: 诊断bug (12m)`——有标题、时长,没 task_id。

agent 看到后想查详情或回报结果,得先跑 `task list` 找 task_id。加一步间接查询。

改为:`task: fix-001 诊断bug (12m)`。agent 直接 `task status fix-001`。

改动:buildRecipientStatus 的 Sprintf 格式串多拼一个 task_id。msg/offline/idle 的格式不变。

### 27c — whoami 命令

新增 `agentlink whoami`——agent 在困惑时唯一需要记住的命令名。输出:

```
You are jiefan:main
Connected to http://localhost:8080

Current: task: fix-001 诊断bug (12m)
Inbox: 2 waiting

Received tasks:
  fix-001  in_progress  诊断bug (12m ago)
  fix-002  suspended    清理日志 (45m ago)

Sent tasks:
  feat-001 issued  加监控 (3m ago) → vm-server:worker

Commands:
  agentlink task status <id>                     — task 详情
  agentlink task result <id> completed "<result>" — 回报结果
  agentlink task list                             — 完整任务列表
  agentlink list --all                            — 团队设备状态
  agentlink send [--interrupt] <target> <content>  — 发消息
```

不排斥和 `task list` 有重叠——`whoami` 是"睁眼看到世界",`task list` 是"细看任务"。服务端新增 `GET /whoami` 聚合上述数据。

### 27d — CLAUDE.md 精简

当前 CLAUDE.md 列了所有命令和一长串规则,长上下文里会被压缩忽略。

精简为一句话锚点:

```
You are agentlink device jiefan, session main.
When unsure about your identity or tasks, run: agentlink whoami
```

不再列任何具体命令——全部交给 `whoami` 和 `list --all` 按需输出。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | 27a: taskSend Lua 加 issued SAdd + handleTaskList 双查询 + handleTaskResult/Cancel 加 issued SRem |
| | 27b: buildRecipientStatus 格式串带 task_id |
| | 27c: 新增 buildWhoami |
| `pkg/api/server.go` | 27c: 注册 GET /whoami |
| `pkg/cli/net/tasks.go` | 27a: RunTaskList 双组显示 |
| `pkg/cli/net/whoami.go` | 27c: 新增 RunWhoami |
| `cmd/agentlink/main.go` | 27c: 注册 whoami 子命令 + printUsage |
| `pkg/adapter/claude.go` | 27d: InitTemplate 返回一行锚点 |

## Acceptance criteria

- [ ] `task list` 显示 Received 和 Sent 两组
- [ ] issued 集合与 tracking 集合生命周期对称(创建时加,完成/取消时删)
- [ ] `current` 格式为 `task: <task_id> <title> (<dur>)`
- [ ] `agentlink whoami` 输出自我身份、当前状态、收到的/发出的 task、命令参考
- [ ] CLAUDE.md 只有一行身份锚点
- [ ] `go test ./... -count=1` 全部通过
