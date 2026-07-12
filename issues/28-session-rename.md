# 28 — session rename

Type: AFK
Blocked by: 无

## 背景

当前只能 `session add <name>` 和 `session remove <name>`,没有改名。用户想重命名一个会话时,需要手动 add 新的 → 等创建完 → remove 旧的,步骤多且中间态容易混乱。

## 设计

`agentlink session rename <old> <new>` 一站式完成:

1. 检查 `<old>` session 存在,`<new>` 不冲突
2. 停掉 `<old>` 的 tmux session 和 poller
3. 在 `BaseDir` 下重命名目录:`mv <old> <new>`
4. 起一个新的 tmux session(`<new>`)和 poller(如果启用)
5. 更新 server 上的 sessions 列表(PATCH /agents/sessions):删除 old,添加 new
6. 更新本地 config.toml 的 `[sessions]` 段:把 old key 换成 new key,保留 session_id

### 注意点

- tmux session name 也要跟着改(从 `<old>` 改为 `<new>`)
- poller session name 从 `<old>-poller` 改为 `<new>-poller`
- 当前如果有 task 在 `<old>` session 上 in_progress,不阻止重命名(重命名后 agent 看不到 context 了是用户的问题)
- CLAUDE.md 文件不改内容,只改文件名(在 `BaseDir/<new>/` 下)
- `--no-poll` 模式跳过 poller 重命名

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/cli/runtime/session.go` | 新增 `RunSessionRename(old, new)` |
| `cmd/agentlink/main.go` | 注册 `session rename` 子命令 + printUsage |

## Guidance 设计(与 issue 28 相关)

`whoami` 是 agent 的操作指南入口,应遵守两条原则:

1. **只展示 agent 可执行的命令**:`install`/`uninstall`/`restart`/`attach` 不列在 whoami 中——agent 不知道就不该跑
2. **渐进式披露**:agent 知道基础命令就够了,不需要完整手册。复杂场景靠 poller 注入引导

当前 whoami 输出缺少 `session *` 系列命令,应补充。

### 当前 whoami 命令列表

```
Commands:
  agentlink task send <target> <id> "<content>"      — issue task (need result)
  agentlink task result <id> completed "<msg>"        — report result (worker)
  agentlink task status <id>                           — task detail
  agentlink task list                                  — full task list
  agentlink list --all                                 — team devices
  agentlink send [--interrupt] <target> "<msg>"       — send msg (no reply)
```

### 应补充

- `agentlink session add <name>` — create new agent session
- `agentlink session remove <name>` — remove agent session
- `agentlink session rename <old> <new>` — rename agent session

## Acceptance criteria

- [ ] `agentlink session rename old new` 成功,目录/tmux/config/server 全部更新
- [ ] rename 后 `agentlink list --all` 显示新 session 名
- [ ] rename 后 poller 正常工作(如果之前启用了)
- [ ] rename 不存在的 session 返回错误
- [ ] rename 到已存在的 name 返回错误
- [ ] whoami 输出包含 session 管理命令
- [ ] whoami 不包含 install/uninstall/restart/attach
- [ ] `go test ./... -count=1` 全部通过
