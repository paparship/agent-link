# 4a — 任务 API 服务端

Type: AFK
Blocked by: 3a

## What to build

任务完整生命周期的服务端 API，包含 5 个端点和收件箱 pull 的 task 感知。

### 数据模型

任务记录独立存储，收件箱只存放通知。

```
Key: task:<task_id>
Type: Hash
Fields:
  task_id       string     # 任务 ID（调用方指定）
  status        string     # issued | in_progress | completed | suspended | cancelled
  assigned_to   string     # "device:session"
  issued_by     string     # "device:session"
  content       string     # 任务内容
  issued_at     string     # ISO 8601
  result        string     # completed/suspended 时填入（可选）
  completed_at  string     # 完成/取消时间
TTL: 7 天
```

任务分配追踪，用于繁忙检查：

```
Key: tasks:<device>:<session>
Type: Set
Members: task_id（当前分配给该 session 的任务，不含已 completed/cancelled 的）
```

收件箱格式（继承自 3a，新增 type=task）：

```json
{
  "id": "uuid",
  "type": "task",             // "msg" | "task"
  "from_device": "jiefan-pc",
  "from_session": "main",
  "task_id": "001",           // type=task 时有
  "content": "修复登录 bug",
  "created_at": "2026-05-03T12:00:00Z"
}
```

### 状态机

```
                    ┌── completed
                    │
issued ──(pull)──→ in_progress ──(result)──┼── suspended ──(resume)──→ in_progress
                                           │
                                           └── (cancel)──→ cancelled
```

| 角色 | 操作 |
|------|------|
| 系统（pull 时自动） | issued → in_progress |
| Agent | in_progress → completed / suspended |
| Main | suspended → in_progress（resume）、suspended → cancelled |

### 端点

#### POST /tasks/send

发放任务。创建任务记录 + 写入目标 inbox。

```
Auth: Bearer <api_key>
Body: {
  "to": "device:session",
  "from_session": "main",
  "task_id": "001",
  "content": "修复登录 bug"
}
Response 200: { "msg_id": "uuid" }
Response 409: { "error": "target has an in_progress task" }
              { "error": "target has 2 suspended tasks" }
```

**校验：**
- to/from_session/task_id/content 必填
- task_id 命名规则同 device（小写字母/数字/连字符/下划线，2-32 位，字母开头）
- content 上限 3000 字符
- 目标 device 和 session 存在性校验（同消息）
- task_id 全局唯一（已存在返回 409）

**繁忙检查：**
- 查询 `tasks:<target_device>:<target_session>` Set 中所有任务
- 逐一查 `task:<id>` 当前 status
- 有 in_progress → 409
- suspended 数量 ≥ 2 → 409
- 通过后正常发放

**发放流程：**
1. 创建 `task:<task_id>` Hash（status=issued，assigned_to，issued_by，content，issued_at）
2. `SADD tasks:<device>:<session>` 添加追踪
3. 构造 inbox 条目（type=task, task_id, content）
4. `LPUSH inbox:<device>:<session>`

#### GET /inbox/pull（修改）

在现有 pull 行为上增加 task 感知。

**新增行为：**
- 拉取到的 item 如果 `type == "task"`：
  - 自动将 `task:<task_id>` 状态设为 in_progress
  - 清除该 session 的 busy 锁（确认无可疑状态）
- 返回内容不变（已有 msg 兼容）

不影响纯消息的拉取。

#### POST /tasks/result

Agent 回报任务结果。

```
Auth: Bearer <api_key>
Body: {
  "task_id": "001",
  "status": "completed",        // completed | suspended
  "result": "bug 已修复"         // completed 时必填，suspended 时为原因
}
Response 200: { "ok": true }
```

**校验：**
- task_id 必填，对应 task 必须存在且 status=in_progress
- status 只能是 completed 或 suspended
- suspended ≥ 2 的检查由 /tasks/send 执行，此处不重复

**流程：**
- status → completed/suspended，记录 result 和 completed_at
- 从 `tasks:<device>:<session>` Set 中移除

#### POST /tasks/resume

Main 恢复挂起的任务。

```
Auth: Bearer <api_key>
Body: {
  "task_id": "001",
  "content": "先创建数据库表，SQL: ..."
}
Response 200: { "ok": true }
```

**校验：**
- task_id 必填，对应 task 必须存在且 status=suspended
- content 必填（新的指导说明），上限 3000

**流程：**
1. task 状态 → in_progress，清除 result
2. `SADD tasks:<device>:<session>` 恢复追踪
3. 构造 inbox 条目重新 LPUSH 到目标 inbox

#### POST /tasks/cancel

Main 取消任务。

```
Auth: Bearer <api_key>
Body: {
  "task_id": "001"
}
Response 200: { "ok": true }
```

**校验：**
- task_id 必填，对应 task 必须存在且 status 不是 completed/cancelled

**流程：**
- status → cancelled，记录 completed_at
- 从 `tasks:<device>:<session>` Set 中移除

#### GET /tasks/status

查询任务状态。

```
GET /tasks/status?task_id=001
Response 200: {
  "task_id": "001",
  "status": "completed",
  "assigned_to": "device:worker",
  "issued_by": "device:main",
  "content": "修复登录 bug",
  "result": "bug 已修复",
  "issued_at": "2026-05-03T12:00:00Z",
  "completed_at": "2026-05-03T12:30:00Z"
}
Response 404: { "error": "task not found" }
```

## Acceptance criteria

### POST /tasks/send

- [ ] 发送合法任务返回 200 + msg_id
- [ ] 目标有 in_progress 任务 → 409
- [ ] 目标有 ≥ 2 条 suspended → 409
- [ ] 目标只有 1 条 suspended → 允许发送
- [ ] 重复 task_id → 409
- [ ] 缺少必填字段 → 400
- [ ] content 超 3000 → 400
- [ ] 目标 device 不存在 → 404
- [ ] 目标 session 不存在 → 404
- [ ] 无 auth → 401
- [ ] Redis 写入 `task:<id>` Hash + SADD `tasks:<device>:<session>` + LPUSH inbox

### GET /inbox/pull（task 感知）

- [ ] 拉取到 task 类型时自动将 `task:<id>` 状态从 issued → in_progress
- [ ] 拉取 msg 类型时无副作用（行为不变）
- [ ] 权限校验同 3a

### POST /tasks/result

- [ ] completed 回报 → task 状态变为 completed
- [ ] suspended 回报 → task 状态变为 suspended
- [ ] 完成后从 `tasks:<device>:<session>` 移除
- [ ] 不存在的 task_id → 404
- [ ] status 不是 completed/suspended → 400
- [ ] 无 auth → 401

### POST /tasks/resume

- [ ] 恢复挂起任务 → status 变回 in_progress
- [ ] 重新写入 target inbox（agent 能 pull 到）
- [ ] 恢复后从 `tasks` Set 验证（确认被重新加入）
- [ ] 不存在的 task_id → 404
- [ ] 当前不是 suspended → 400
- [ ] 无 auth → 401

### POST /tasks/cancel

- [ ] 取消任务 → status cancelled
- [ ] 从 `tasks` Set 移除
- [ ] 不存在的 task_id → 404
- [ ] 已 completed/cancelled → 400
- [ ] 无 auth → 401

### GET /tasks/status

- [ ] 存在时返回完整任务信息
- [ ] 不存在时返回 404
- [ ] 无 auth → 401

### 全生命周期

- [ ] send → pull → in_progress → result completed，状态追踪完整
- [ ] send → pull → result suspended → resume → pull → result completed（完整挂起恢复）
- [ ] send → cancel，跳过执行

## Blocked by

- Blocked by #3a
