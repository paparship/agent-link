# Messaging System — Implementation Plan

对照 `docs/messaging-design.md`,列从现状到目标需要的变更、测试边界、验收点。

## 现状速览

**已经对的(不用改)**:
- msg/task 都支持 `--interrupt`,全链路通(CLI → net → server → poller)
- poller RPop 时 server 自动置 task `in_progress`(handlers.go handlePull)
- msg send 不检查 busy,直接进 inbox
- poller 注入 msg 时带 `[来自 X:Y 的消息]` 前缀
- poller 自动接受 trust prompt、自动心跳

**现状的 gap(要改的)**:
- busy 检查只看 `in_progress`,不看 `issued` → 并发窗口能堆 task
- busy 检查 + 写 task 非原子 → 多请求并发都通过检查
- poller 注入 task 时无引导提示(裸内容)→ agent 不知道怎么回报
- interrupt 打断后原 task 状态不更新 → bug,永远 busy
- `recipient_status` 是拆分字段(Status/CurrentTask/TaskDuration/InboxDepth/LastSeen)→ 要简化成单字段 `current`
- `message status` 命令 + `agentlink:msg:<id>` 持久化 + `delivered_at` → msg fire-and-forget,要砍
- task/msg 无 title 字段 → `current` 显示需要

## 变更清单(按依赖顺序)

### 1. task send 原子化 + issued 也挡新 task

**文件**:`pkg/api/handlers.go` `handleSendTask`

**现状**:分三步——SMembers 遍历查 in_progress → HSet task record → SAdd tracking set → LPush inbox。中间有并发窗口。

**改为**:用 Lua 脚本一次性完成。

```lua
-- KEYS[1] = agentlink:tasks:<device>:<session>
-- KEYS[2] = agentlink:task:<task_id>
-- ARGV[1] = task_id
-- ARGV[2] = now (RFC3339)
-- ARGV[3] = assigned_to
-- ARGV[4] = issued_by
-- ARGV[5] = content
-- ARGV[6] = inbox_key
-- ARGV[7] = inbox_item_json

-- 检查 issued 或 in_progress
local members = redis.call('SMEMBERS', KEYS[1])
for _, tid in ipairs(members) do
  local st = redis.call('HGET', 'agentlink:task:' .. tid, 'status')
  if st == 'issued' or st == 'in_progress' then
    return {0, 'busy', tid, st}  -- 拒绝,返回冲突的 task
  end
end

-- 检查 suspended 上限(保留现有逻辑)
local suspended = 0
for _, tid in ipairs(members) do
  local st = redis.call('HGET', 'agentlink:task:' .. tid, 'status')
  if st == 'suspended' then suspended = suspended + 1 end
end
if suspended >= 2 then
  return {0, 'suspended_limit', '', ''}
end

-- 写 task record
redis.call('HSET', KEYS[2],
  'task_id', ARGV[1],
  'status', 'issued',
  'assigned_to', ARGV[3],
  'issued_by', ARGV[4],
  'content', ARGV[5],
  'issued_at', ARGV[2])
redis.call('EXPIRE', KEYS[2], 604800)
redis.call('SADD', KEYS[1], ARGV[1])
redis.call('LPUSH', ARGV[6], ARGV[7])
redis.call('EXPIRE', ARGV[6], 604800)
return {1, '', '', ''}
```

Go 端用 `s.rdb.Eval(ctx, script, keys, args...)` 调用。返回值解析:第一个元素 1=成功,0=拒绝(第二个元素是原因 busy/suspended_limit)。

**砍掉**:handleSendTask 里现有的 SMembers 遍历 + HSet + SAdd + LPush 分步逻辑,全部进 Lua。

### 2. poller 注入 task 带引导提示

**文件**:`pkg/cli/runtime/poller.go` `Run` 里注入逻辑(约 73-84 行)

**现状**:task 注入是裸 content,msg 注入带 `[来自 X:Y 的消息]` 前缀。

**改为**:task 注入带完整引导:

```go
injectContent := msg.Content
if msg.Type == "msg" {
    prefix := fmt.Sprintf("[来自 %s:%s 的消息] ", msg.FromDevice, msg.FromSession)
    injectContent = prefix + msg.Content
} else if msg.Type == "task" {
    injectContent = fmt.Sprintf(
        "[来自 %s:%s 的任务 %s]\n%s\n完成后请执行: agentlink task result %s completed \"<结果>\"\n如需挂起: agentlink task result %s suspended \"<原因>\"",
        msg.FromDevice, msg.FromSession, msg.TaskID,
        msg.Content,
        msg.TaskID, msg.TaskID,
    )
}
```

agent 看到 task 内容的同时就看到回报命令,不用 CLAUDE.md 教。

### 3. interrupt 原子置 suspended

**文件**:`pkg/api/handlers.go` `handleSend` 和 `handleSendTask`

**现状**:interrupt 消息只是 `Interrupt: true` 写进 Message,LPUSH inbox。原 in_progress task 状态不变。

**改为**:发送 interrupt 消息时,server 先把目标 session 的 in_progress task 置 suspended,再 LPUSH。用 Lua 原子完成:

```lua
-- KEYS[1] = agentlink:tasks:<device>:<session>
-- ARGV[1] = inbox_key
-- ARGV[2] = inbox_item_json
-- ARGV[3] = now

local members = redis.call('SMEMBERS', KEYS[1])
for _, tid in ipairs(members) do
  local st = redis.call('HGET', 'agentlink:task:' .. tid, 'status')
  if st == 'in_progress' then
    redis.call('HSET', 'agentlink:task:' .. tid,
      'status', 'suspended',
      'suspended_at', ARGV[3])
  end
end
redis.call('LPUSH', ARGV[1], ARGV[2])
redis.call('EXPIRE', ARGV[1], 604800)
return 1
```

handleSend 和 handleSendTask 在 `req.Interrupt == true` 时调这个脚本;非 interrupt 走原来的 LPUSH 路径(但 task send 的 LPUSH 已在变更 1 的脚本里,msg send 的 LPUSH 保持简单)。

### 4. 砍 message status / msg 持久化 / delivered_at

**文件**:
- `pkg/api/handlers.go`:删 `handleMsgStatus`(1105 行起)、删 handleSend/handleSendTask 里写 `agentlink:msg:<id>` 的 HSet+Expire、删 handlePull 里更新 `delivered_at` 的逻辑
- `pkg/api/server.go`:删 `GET /messages/status` 路由(41 行)
- `pkg/cli/net/messages.go`:删 `RunMessageStatus`
- `pkg/cli/messages.go`(显示层):删 `RunMessageStatus` 的显示逻辑
- `cmd/agentlink/main.go`:删 `cmdMessage` 和 `message status` 子命令、删 printUsage 里的 `message status` 行

**保留**:msg 的 `id` 字段(send 响应返回,用于发送方日志),但不再持久化 msg 记录。

### 5. recipient_status 简化为 current 单字段

**文件**:`pkg/api/handlers.go` `buildRecipientStatus` 和 `RecipientStatus` 结构

**现状**:
```go
type RecipientStatus struct {
    Device, Session, Status, CurrentTask, TaskDuration, LastSeen string
    InboxDepth int
}
```

**改为**:
```go
type RecipientStatus struct {
    Device  string `json:"device"`
    Session string `json:"session"`
    Current string `json:"current"`  // "idle" / "msg: <title> (<dur>)" / "task: <title> (<dur>)" / "offline (<dur>)"
}
```

构造逻辑:
1. 查 `tasks:<d>:<s>` 集合,有 in_progress → `current = "task: <title> (<dur>)"`,title 从 task record 的 title 字段取(变更 6)
2. 无 in_progress,查 `current_msg:<d>:<s>` key 存在 → `current = "msg: <title> (<dur>)"`
3. 都无,查 `last_seen` 心跳:
   - < 120s → `current = "idle"`
   - ≥ 120s → `current = "offline (<dur>)"`

### 6. current_msg 追踪

**文件**:`pkg/cli/runtime/poller.go` 和 `pkg/api/handlers.go`

poller 注入 msg 时,调一个新 API `POST /inbox/ack` 通知 server"我正在处理这条 msg"。server 设 `current_msg:<d>:<s>` = msg_id + title + started_at,TTL 10 分钟(防 poller 崩了 key 残留)。

poller 下次取到新消息(任何类型)时,先调 `POST /inbox/clear-current` 清掉 current_msg,再处理新的。

**简化版**:不引入新 API,poller 注入 msg 时直接调现有的一个轻量 endpoint,或者干脆让 server 在 `handlePull` 返回 msg 时就设 current_msg(pull 即设,pull 下一条时清上一条)。

**倾向后者**:`handlePull` 里 RPop 出 msg 后,如果是 type=msg,设 `current_msg:<d>:<s>`(带 title + started_at,TTL 10m)。下次 handlePull 再被调,先清 current_msg,再设新的(或无消息就清)。poller 不用改,server 端自洽。

### 7. title 字段

**文件**:
- `pkg/api/handlers.go`:`SendRequest`/`SendTaskRequest` 加 `Title string \`json:"title,omitempty"\``,task record HSet 加 `title` 字段,msg 的 Message 结构加 `Title`
- `pkg/cli/net/messages.go`/`tasks.go`:`RunSend`/`RunTaskSend` 加 title 参数
- `cmd/agentlink/main.go`:`send`/`task send` 加 `--title` flag
- `pkg/cli/runtime/poller.go`:注入时从 msg.Title 取标题(前缀里用)

**默认值**:task 不填 title → 用 task_id;msg 不填 title → 用 content 前 40 字符。在 server 端补默认值,不传到客户端。

## 测试边界和验收点

### 变更 1:task send 原子化 + issued 挡新 task

**测试要覆盖**:

- `TestTaskSend_busy_on_issued`:先发 task A(成功,状态 issued),再发 task B 到同一 session → 409 busy,响应含 `recipient_status`。inbox 里只有 task A,task B 没进。
- `TestTaskSend_busy_on_in_progress`:模拟 task A 被 pull(变 in_progress),再发 task B → 409 busy。
- `TestTaskSend_concurrent`:用 goroutine 并发发 10 个 task 到同一 session,断言只有 1 个成功,其余 9 个 409。**这是原子化的核心验收点**——非原子实现下会有多个成功。
- `TestTaskSend_suspended_limit`:保留现有"suspended ≥ 2 拒绝"的测试。
- `TestTaskSend_success_path`:成功路径完整——task record 字段齐全、tracking set 有、inbox 有、status=issued、TTL 正。

**验收**:并发场景下严格一个 session 一个待处理 task。`go test -race` 通过。

### 变更 2:poller 注入 task 带引导

**测试要覆盖**:

- `TestPoller_injectTask_hasGuidance`:mock sendKeys 捕获注入内容,发一个 task,poller 注入后断言内容包含:
  - `[来自 <device>:<session> 的任务 <task_id>]`
  - task content
  - `agentlink task result <task_id> completed`
  - `agentlink task result <task_id> suspended`
- `TestPoller_injectMsg_hasPrefix`:msg 注入包含 `[来自 X:Y 的消息]`(现有逻辑,确认不破坏)。

**验收**:agent 收到 task 注入能看到完整回报指引。不依赖 CLAUDE.md。

### 变更 3:interrupt 原子置 suspended

**测试要覆盖**:

- `TestInterrupt_setsOriginalTaskSuspended`:先发 task A,pull 让它 in_progress,再发 interrupt msg → 响应 200。查 task A 状态 = `suspended`(不是 in_progress)。inbox 里有 interrupt 消息。
- `TestInterrupt_noInProgressTask`:无 in_progress task 时发 interrupt msg → 正常进 inbox,无 task 被置 suspended(不报错)。
- `TestInterrupt_taskAlsoSuspends`:发 task A in_progress,再发 `task send --interrupt` task B → task A suspended,task B 进 inbox。

**验收**:interrupt 后原 task 状态正确,不会"永远 busy"。

### 变更 4:砍 message status

**测试要覆盖**:

- `TestMessageStatus_removed`:`GET /messages/status?id=xxx` → 404(路由删除)。
- `TestSend_noMsgPersistence`:发 msg 后,`agentlink:msg:<id>` key 不存在。
- `TestPull_noDeliveredAt`:pull 后 msg record 不更新 delivered_at(因为 record 根本不存在)。

**验收**:msg 不留持久记录,`message status` 命令消失。

### 变更 5+6:current 字段 + current_msg 追踪

**测试要覆盖**:

- `TestRecipientStatus_idle`:无 task、无 current_msg、心跳新鲜 → `current = "idle"`。
- `TestRecipientStatus_task`:有 in_progress task → `current = "task: <title> (Xm)"`。
- `TestRecipientStatus_msg`:current_msg key 存在 → `current = "msg: <title> (Xs)"`。
- `TestRecipientStatus_offline`:心跳过期 → `current = "offline (Xm ago)"`。
- `TestCurrentMsg_setOnPull`:pull 出 msg 后,`current_msg:<d>:<s>` key 存在,含 title + started_at。
- `TestCurrentMsg_clearedOnNextPull`:第二次 pull(空或新消息)后,旧 current_msg 清掉。

**验收**:`list --all` 返回的每个 session 有 `current` 字段,四种取值正确。`recipient_status` 响应同样是单字段。

### 变更 7:title 字段

**测试要覆盖**:

- `TestTaskSend_withTitle`:发 task 带 `--title` → task record 有 title,recipient_status 的 current 显示该 title。
- `TestTaskSend_defaultTitle`:不发 title → title 默认 = task_id。
- `TestMsgSend_withTitle`:发 msg 带 title → current_msg 用该 title。
- `TestMsgSend_defaultTitle`:msg 不带 title → title 默认 = content 前 40 字符。

**验收**:title 默认值合理,显示正确。

## 实施顺序

按依赖关系:

1. **变更 7(title 字段)**先做——后续 current 显示和注入引导都依赖 title。但要先定 title 默认值逻辑。
2. **变更 1(task send 原子化)**——核心并发安全。
3. **变更 3(interrupt 置 suspended)**——bug 修复,独立。
4. **变更 2(poller 注入引导)**——依赖 title 字段(引导里可用 title,但当前设计用 task_id 也够,不强依赖)。
5. **变更 6(current_msg 追踪)**——handlePull 里设/清。
6. **变更 5(recipient_status 简化)**——依赖 current_msg 和 title。
7. **变更 4(砍 message status)**——最后,纯删除,不影响其他。

每步做完跑 `go test ./... -count=1 -race` 确保不破坏现有测试,新测试覆盖该步。

## 不在本次范围

- **CLAUDE.md 边界文档**:poller 注入引导已覆盖入门,CLAUDE.md 留待后续。
- **task 队列分拆**:inbox 混排不变。
- **msg 置忙**:msg 不进 tasks 集合,current 字段诚实显示即可。
- **并发 interrupt 的优先级**:多个 interrupt 同时到达按 FIFO,不判优先级。
