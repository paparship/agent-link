# Messaging System Design

## Core Model

两个原语:**msg** 和 **task**。

- **msg** — 发完不求回报。旁路 busy 检查,直接进 inbox。agent 看完即弃。
- **task** — 要回报。有 lifecycle。有 task_id。一个 session 同时只允许一个"待处理或处理中"的 task。

区分 msg/task 不是因为它们对 agent 的占用不同(都占用),而是因为对回报的期望不同:task 要结果,msg 不要。这个区分让调度者知道"派出去之后要不要等回来"。

## 消息 (msg)

- **无 lifecycle**:发出就完,不追踪状态,不要求回报
- **旁路 busy**:进 inbox 不检查 task 状态。通知类信息不能被 busy 挡
- **混排 FIFO**:和 task 共享 inbox,先到先处理
- **可 interrupt**:`--interrupt` 的 msg 不排队,Ctrl+C 打断当前执行后注入
- **注入带前缀**:`[来自 X:Y 的消息] <content>`,让 agent 区分队列注入和用户输入
- **用 msg 的场景**:通知、配置、上下文、中断。不求回报,看一眼即可
- **禁用**:用 msg 传递需要回报的指令。msg 无 task_id,回报无法关联,发送方永远等不到结果

## 任务 (task)

### 状态表

| From | Action | To | 字段变化 | side effects |
|------|--------|----|---------|-------------|
| (新建) | `task send <target> <task_id> <content>` | `issued` | status, assigned_to, issued_by, content, title, issued_at | SAdd tracking + issued 集合; LPUSH inbox |
| `issued` | pull / poller | `in_progress` | status | — |
| `in_progress` | `task result <task_id> completed "<result>"` | `completed` | status, result, completed_at | SRem tracking + issued; 通知发起人 |
| `in_progress` | `task result <task_id> suspended "<reason>"` | `suspended` | status, result, completed_at | SRem tracking + issued; 通知发起人 |
| `in_progress` | `task cancel <task_id>` | `cancelled` | status, completed_at | SRem tracking + issued; 通知 worker |
| `suspended` | `task cancel <task_id>` | `cancelled` | status, completed_at | SRem tracking + issued; 通知 worker |
| `issued` | `task cancel <task_id>` | `cancelled` | status, completed_at | SRem tracking + issued; **不通知**(pull 过滤跳过) |
| `completed` | `task cancel <task_id>` | **400** | — | — |
| `cancelled` | `task cancel <task_id>` | **400** | — | 幂等 |
| `suspended` | `task resume <task_id> <guidance>` | `issued` | status, content=guidance, issued_at, result="", completed_at="" | SAdd tracking + issued; LPUSH inbox |
| `completed` | `task reopen <task_id> <reason>` | `issued` | status, result="", completed_at="", reopen_reason | SAdd tracking + issued; LPUSH inbox(带 reason 前缀) |
| `cancelled` | `task reopen <task_id> <reason>` | `issued` | 同上 | 同上 |
| `in_progress` | `task reopen <task_id>` | **400** | — | — |
| `suspended` | `task reopen <task_id>` | **400** | — | — |
| (不存在) | 任意操作 | **404** | — | — |

> **术语**:`SAdd` = Redis Set Add; `SRem` = Set Remove; `LPUSH` = 队列左侧推入(FIFO); `tracking` 集合 = `agentlink:tasks:<d>:<s>`(收到的 task); `issued` 集合 = `agentlink:issued:<d>:<s>`(发出的 task)。

### 关键设计

- **agent 只回报,不管理状态**:`issued`→`in_progress` 是 server 在 pull 时自动切换。agent 只需 `task result completed/suspended`。cancel/resume/reopen 都是发起人视角的操作,worker 不需要也不应该自己执行。
- **返回 issued**:resume 和 reopen 都回到 `issued`,重新进入 inbox 排队,走正常 pull→in_progress 流程。
- **静默 cancel issued**:取消还没被拉取的任务时不发通知。pull 端过滤跳过 stale inbox item,worker 无感知。

### 注入引导

poller 注入 task 时,内容带完整操作指南:

```
[来自 main:main 的任务 fix-001]
查 prod 为什么 500,找到根因
完成后请执行: agentlink task result fix-001 completed "<结果>"
如无法完成需挂起: agentlink task result fix-001 suspended "<原因>"
```

agent **不靠 CLAUDE.md** 也能参与 task 协作。提示本身就是操作指南。

### 自动通知

`task result` 和 `task cancel` 成功后,server 自动往对方 inbox 推一条 msg 通知:

| 场景 | 方向 | 通知 title | 通知 content |
|------|------|-----------|-------------|
| worker 回报 completed/suspended | worker → 发起人 | `任务回报 <task_id>` | `completed/suspended: <result>` |
| 发起人 cancel in_progress/suspended | 发起人 → worker | `任务回报 <task_id>` | `cancelled` |

通知是普通 msg(server 额外 LPUSH),worker 不需要手动发。

## Busy & Scheduling

**一个 session 同时只允许一个"待处理或处理中"的 task。**

```
if exists task in tasks:<d>:<s> with status in (issued, in_progress):
    return 409 busy
```

`issued` 也挡新 task——不只 `in_progress`。这个检查 + 写入用 Lua 脚本原子化,消除并发窗口。

| `current` | 能派 task? | 说明 |
|-----------|-----------|------|
| `idle` | ✓ | 立刻处理 |
| `msg (短)` | ✓ | 等 msg 完,很快 |
| `msg (长)` | ✓ 但可疑 | 可能卡死,考虑 interrupt |
| `task` | ✗ | busy 检查会拒;必要时 interrupt |
| `offline` | ✗ | 不可达,消息进 inbox 等上线 |

msg 旁路 busy 是特性。server 409 只看 task 状态,不看 current_msg。

## Agent State: `current`

agent 单线程意味着"正在做什么"是调度关键信息。server 向调度者暴露一个 `current` 字段:

| `current` | 含义 |
|-----------|------|
| `idle` | 空闲 |
| `msg: <title> (<duration>)` | 正在处理一条 msg |
| `task: <task_id> <title> (<duration>)` | 正在执行一个 task |
| `offline (<duration>)` | 心跳过期 |

**推导机制**(在 `buildRecipientStatus` 中):
1. 查 `agentlink:tasks:<d>:<s>` 集合中有无 `in_progress` 的 task → 有则显示 `task: <task_id> <title> (时长)`
2. 无 → 查 `agentlink:current_msg:<d>:<s>` key 是否存在 → 有则显示 `msg: <title> (时长)`
3. 无 → 查心跳是否过期 → 过期显示 `offline`,否则 `idle`

**current_msg 的设与清**:在 `handlePull` 中完成——每次 pull 先清理旧 key,如果返回的是 msg 则设新 key(TTL 10min)。poller 不参与,它只做"调 HTTP → 推 tmux"。

`current` 诚实反映 agent 占用——不管 msg 还是 task,处理中都显示。解决 context 压缩后遗忘"谁在干什么"的问题。

## Inbox & Poller

- **FIFO**:先到的先处理,公平
- **混排**:msg 和 task 共享一个队列,不分队列
- **interrupt 插队**:`--interrupt` 的 msg/task 不排队,Ctrl+C 打断当前执行后注入。任何人都能 interrupt 任何人,系统不替发送方决定紧急程度。被打断的 in_progress task 原子置 suspended。
- **poller 循环**(默认间隔 5s):`POST /heartbeat` → `GET /inbox/pull?limit=1` → 有 item 则 waitForIdle(检测 tmux pane,1s/次) → 注入 → sleep 5s。poller 不知道 item 是 msg 还是 task,不设任何状态。
- **混排消费示例**:发来顺序 `msg-A → task-C → msg-B`,轮询逐个 RPop 为 `msg-A → task-C → msg-B`,无差别消费。
- **Pull 过滤**:poller 或手动 pull 取到 task 类型 item 后,先查 task record 的 status。非 `issued`(cancelled/suspended/completed) 的 item 跳过且不消耗 limit。
- **inbox 里实际主要是 msg**:task 被 busy 检查拦在 inbox 外,inbox 同一时刻最多一个 task。

## Issued 反向索引

server 维护 `agentlink:issued:<from_device>:<from_session>` 集合,记录谁发出了哪些 task。与 `agentlink:tasks:<d>:<s>`(收到)对称。

| 事件 | tasks 集合(收到) | issued 集合(发出) |
|------|-----------------|-------------------|
| task send | SAdd target | SAdd sender |
| task result | SRem target | SRem sender |
| task cancel | SRem target | SRem sender |
| task resume | SAdd target | SAdd sender |
| task reopen | SAdd target | SAdd sender |

`agentlink task list` 同时查询两个集合,分两段显示:

```
Received tasks:
  fix-001  in_progress  诊断bug
Sent tasks:
  feat-001  issued  加监控  → vm-server:worker
```

## Agent Self-Identity Recovery

agent 在 context 压缩后遗忘身份和任务是核心问题。三个机制联动解决:

**whoami 命令**:`agentlink whoami` 输出当前身份、设备、session、current 状态、收到的/发出的 task、常用命令参考。server 端 `GET /whoami` 聚合 current + tracking + issued 数据。

**CLAUDE.md 精简**:不再罗列所有命令,只有一行身份锚点。两原因:一是 agent 不需要记多个命令,只记一个 `agentlink whoami` 即可渐进式获取全部信息;二是 CLAUDE.md 是用户的系统 prompt 空间,通讯工具不应该占用太多。
```
You are agentlink device **jiefan**, session **main** on the team network.
When involving agent collaboration network, run `agentlink whoami` first.
```
所有命令细节由 whoami 和 poller 注入提示按需呈现。agent 不靠 CLAUDE.md 记忆语法。

**current 字段带 task_id**:agent 看到 `task: fix-001 诊断bug (12m)` 可直接 `task status fix-001`,不需要先跑 `task list` 找 ID。
