# 4b — CLI task 子命令

Type: AFK
Blocked by: 4a, 3b

## What to build

`agentlink task` 子命令体系，覆盖任务的完整生命周期。

### 命令树

所有 `task` 子命令共享前缀 `agentlink task`：

```
agentlink task send     <target> <task_id> "<content>"
agentlink task result   <task_id> <status> "<result>"
agentlink task resume   <task_id> "<guidance>"
agentlink task cancel   <task_id>
agentlink task status   <task_id>
```

### 实现说明

每个子命令读取配置的方式与 3b（send/pull）相同：
- `~/.agentlink/config.toml` → server、device
- `~/.agentlink/credentials.json` → api_key
- 当前 session 的 `.agentlink.toml`（逐级向上查找）→ session 名

### agentlink task send

```
agentlink task send worker "001" "修复登录 bug"
```

- target 支持短名补全（同 `agentlink send`）
- task_id、content 为必填
- 调用 `POST {server}/tasks/send`
- 成功 → 打印 `✓ Task 001 sent to device:worker`
- 失败 → 打印错误，退出码 1

### agentlink task result

```
agentlink task result "001" completed "bug 已修复"
agentlink task result "001" suspended "需要更多信息"
```

- status 可选 `completed` / `suspended`
- result 为结果描述
- 调用 `POST {server}/tasks/result`
- 成功 → 打印 `✓ Task 001 completed` / `✓ Task 001 suspended`
- 失败 → 打印错误，退出码 1

### agentlink task resume

```
agentlink task resume "001" "先创建数据库表，SQL: ..."
```

- 新 guidance 为必填
- 调用 `POST {server}/tasks/resume`
- 成功 → 打印 `✓ Task 001 resumed`
- 失败 → 打印错误，退出码 1

### agentlink task cancel

```
agentlink task cancel "001"
```

- 调用 `POST {server}/tasks/cancel`
- 成功 → 打印 `✓ Task 001 cancelled`
- 失败 → 打印错误，退出码 1

### agentlink task status

```
agentlink task status "001"
```

- 调用 `GET {server}/tasks/status?task_id=001`
- 有结果 → 打印：

  ```
  Task:      001
  Status:    completed
  Assigned:  device:worker
  Issued by: device:main
  Content:   修复登录 bug
  Result:    bug 已修复
  Issued:    2026-05-03T12:00:00Z
  Completed: 2026-05-03T12:30:00Z
  ```

- 不存在 → 打印 `Task 001 not found`，退出码 1
- 失败 → 打印错误，退出码 1

## Acceptance criteria

### agentlink task send

- [ ] 发送任务成功，打印确认信息
- [ ] 短名自动补全
- [ ] 任务 ID 传递正确
- [ ] content 含空格正常工作
- [ ] 服务端 409 → 打印错误信息
- [ ] 配置/凭证文件缺失 → 报错退出

### agentlink task result

- [ ] completed 回报成功
- [ ] suspended 回报成功
- [ ] 不存在的 task_id → 打印服务端错误

### agentlink task resume

- [ ] 恢复成功
- [ ] guidance 传递正确
- [ ] 不存在的 task_id → 打印服务端错误

### agentlink task cancel

- [ ] 取消成功
- [ ] 不存在的 task_id → 打印服务端错误

### agentlink task status

- [ ] 显示完整任务信息
- [ ] 任务不存在时提示 not found

### 集成场景

- [ ] send → pull → result completed，CLI 全链路过
- [ ] send → pull → result suspended → resume → pull → result completed，CLI 全链路过

## Blocked by

- Blocked by #4a
- Blocked by #3b
