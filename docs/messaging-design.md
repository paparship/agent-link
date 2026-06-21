# Messaging System Design

## Core Model

两个原语:**msg** 和 **task**。

- **msg** — 发完不求回报。旁路 busy 检查,直接进 inbox。agent 看完即弃。
- **task** — 要回报。有 lifecycle。有 task_id。一个 session 同时只允许一个"待处理或处理中"的 task。

为什么是两个,不是一个:agent 是**单线程**的——一个 tmux pane 同一时刻只能做一件事。msg 的"fire-and-forget"是 server 端语义,agent 端没有 fire-and-forget,收到 msg 也要读要想也要占用进程。

区分 msg/task 不是因为它们对 agent 的占用不同(都占用),而是因为**对回报的期望不同**:task 要结果,msg 不要。这个区分让调度者知道"派出去之后要不要等回来"。

## Task State Model

```
issued ──→ in_progress ──→ completed
              │
              ├──→ suspended ──→ (resume) ──→ in_progress
              │
              └──→ cancelled
```

| 状态 | 含义 | 谁触发 | 何时触发 |
|------|------|--------|----------|
| `issued` | 已发送,在 inbox 里,等 poller 取 | server | `task send` 成功时 |
| `in_progress` | 被 poller 取出,正在注入/执行 | server(自动) | poller RPop 时 |
| `completed` | 完成,有结果 | agent | `task result <id> completed <result>` |
| `suspended` | 挂起,等指导 | agent | `task result <id> suspended <reason>` |
| `cancelled` | 取消 | 发送方 | `task cancel <id>` |
| `suspended → in_progress` | 恢复 | 发送方 | `task resume <id> <guidance>` |

**关键设计:agent 不手动管状态。**

- `issued → in_progress`:server 在 poller RPop 时自动切换,agent 无感
- `in_progress → completed/suspended`:agent 被**注入时的提示**引导着回报,不需要记命令
- `cancelled` / `resume`:发送方主动操作,不靠 agent

### 注入时引导

poller 注入 task 时,内容带前缀,告诉 agent 这是个 task、task_id 是什么、完成后怎么回报:

```
[来自 main:main 的任务 fix-001]
查 prod 为什么 500,找到根因
完成后请执行: agentlink task result fix-001 completed "<结果>"
如无法完成需挂起: agentlink task result fix-001 suspended "<原因>"
```

这样 agent **不需要 CLAUDE.md 教**也能参与任务系统——提示本身就是操作指南。agent 看到任务内容的同时就看到回报命令,按提示执行即可。

**收益**:agent 的"入门成本"归零。任何 agent(不限于 Claude Code,只要能读 tmux 输入能执行 shell 命令)都能参与 task 协作,不用预装提示词。

## Msg Basic Rules

- **无 lifecycle**:发出就完,不追踪状态,不要求回报
- **旁路 busy**:进 inbox 不检查 task 状态。通知类信息不能被 busy 挡(否则"数据库连接池改成 50"这种紧急配置在 agent 跑长任务时进不去)
- **混排 FIFO**:和 task 共享 inbox,先到先处理
- **可 interrupt**:`--interrupt` 的 msg 不排队,Ctrl+C 打断当前执行后注入
- **注入带前缀**:`[来自 X:Y 的消息] <content>`,让 agent 区分队列注入和用户输入

### 用 msg 的场景

通知、配置、上下文、中断。不求回报,看一眼即可。

### 禁用

用 msg 传递需要回报的指令。msg 无 task_id,回报无法关联,发送方永远等不到结果。

## Agent State: `current`

agent 单线程意味着"正在做什么"是调度关键信息。server 向调度者暴露一个 `current` 字段:

| `current` | 含义 |
|-----------|------|
| `idle` | 空闲,无 inbox 处理中,无 task 在跑 |
| `msg: <title> (<duration>)` | 正在处理一条 msg |
| `task: <title> (<duration>)` | 正在执行一个 task |
| `offline (<duration>)` | 心跳过期,agent 不可达 |

这个字段**诚实反映 agent 占用状态**——不管 msg 还是 task,处理中都显示。解决两个问题:

- **主 agent 遗忘 context**:context 压缩后忘了派过什么,查 `list --all` 看 `current` 就知道
- **msg 卡死可见**:`msg: xxx (8m)` 一看就异常,可干预

## Busy 语义

**一个 session 同时只允许一个"待处理或处理中"的 task。**

busy 检查(task send 时):

```
if exists task in tasks:<d>:<s> with status in (issued, in_progress):
    return 409 busy
```

`issued` 也挡新 task——不只 `in_progress`。这避免并发窗口:agent 处理 msg 时,Main A 发 task 进 inbox(issued),Main B 同时发 task **必须被拒**,不能让两个 task 都以为自己派成功了。

检查 + 写入 + 置 issued 用 **Lua 脚本原子化**,消除"检查通过但还没写入时另一个请求也通过"的并发窗口。

## Scheduling Decision

调度者根据 `current` 自己决定,server 不强制(除了 task send 的 busy 检查):

| `current` | 能派 task? | 说明 |
|-----------|-----------|------|
| `idle` | ✓ | 立刻处理 |
| `msg (短)` | ✓ | 等 msg 完,很快 |
| `msg (长)` | ✓ 但可疑 | 可能卡死,考虑 interrupt 或换 agent |
| `task` | ✗ | busy 检查会拒;必要时 interrupt |
| `offline` | ✗ | 不可达,消息进 inbox 等上线 |

server 端 409 busy 检查**只看 task 状态**(issued 或 in_progress),不看 current_msg。msg 旁路 busy 是特性。

## Inbox Semantics

- **FIFO**:先到的先处理,公平
- **混排**:msg 和 task 共享一个队列,不分队列
- **interrupt 插队**:`--interrupt` 的 msg/task 不排队,Ctrl+C 打断当前执行后注入
- **interrupt 无优先级判定**:任何人都能 interrupt 任何人,系统不替发送方决定紧急程度。interrupt 就是"我判断这事紧急,强制打断",发送方自己负责判断。滥用 interrupt 打断重要 task 是发送方的问题,不是系统的问题。
- **interrupt 后原 task 置 suspended**:被打断的 in_progress task 不能留在 in_progress(否则永远 busy),置 suspended 让发送方可 resume 或 cancel。server 在处理 interrupt 消息时原子完成(LPUSH + 置 suspended 一起)
- **inbox 里实际主要是 msg**:task 被 busy 检查(issued 也挡)拦在 inbox 外,inbox 同一时刻最多一个 task(就是即将被处理的那个)

## Title

每个 msg/task 带一个短标题,用于 `current` 显示:

- task:`title` 可选,不填默认用 `task_id`
- msg:`title` 可选,不填默认用 content 前 40 字符

标题让调度者扫 `list --all` 时一眼看清每个 agent 在干什么,不用翻 task_id 或 content。

## What to Change (vs Current)

现有代码离这个设计很近,主要差距:

1. **task send 原子化 + issued 也挡新 task**:busy 检查从"只看 in_progress"改成"看 issued OR in_progress",用 Lua 脚本原子完成(检查 + 写 task + 置 issued + SAdd)。消除并发窗口。
2. **poller 注入 task 带引导提示**:注入 task 时内容带前缀 `[来自 X:Y 的任务 <task_id>]` + 回报命令提示。agent 不靠 CLAUDE.md 也能参与协作。
3. **poller 注入 msg 带前缀**(已有,确认保留):`[来自 X:Y 的消息] <content>`。
4. **`current` 字段**:recipient_status 简化成单字段 `current`(idle/msg/task/offline),加 current_msg 追踪(poller 注入 msg 时设 key,下次轮询清)。
5. **title 字段**:task/msg 加可选 title。
6. **interrupt 原子置 suspended**:interrupt 打断后原 in_progress task 置 suspended(现在是 bug,状态没更新)。server LPUSH + 置 suspended 原子完成。
7. **砍 `message status` 命令 + `agentlink:msg:<id>` 持久化 + `delivered_at`**:msg 是 fire-and-forget,不追踪持久状态。

## Not in Scope

- **CLAUDE.md 边界文档**:因为 poller 注入 task 时已带引导提示,agent 不靠 CLAUDE.md 也能协作。CLAUDE.md 的 msg/task 边界说明变成"锦上添花",不是必需。可后补,不阻塞核心改动。
- **task 队列分拆**:inbox 混排 FIFO 不变。task 被 busy 检查拦在 inbox 外,inbox 同一时刻最多一个 task,不需要单独的 task 队列。
- **msg 置忙**:msg 不进 tasks 集合,不阻挡新 task。但 `current` 字段诚实显示 msg 处理状态,调度者自己判断。
