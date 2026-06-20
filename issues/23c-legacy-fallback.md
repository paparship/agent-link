# 23c — 兼容旧配置（无 [sessions] 时用 --continue fallback）

Type: AFK
Blocked by: 23b

## 背景

23a 上线前创建的 config.toml 没有 `[sessions]` 段。这些设备重启后执行 `agentlink resume` 时，`config.Sessions` 为 nil，无法用 `--resume <session_id>` 精确恢复。需要 fallback 让旧配置也能 resume，而不是直接报错要求重新 init。

## 改动

### 1. RunResume 的 fallback 逻辑

`pkg/cli/resume.go` — `RunResume` 遍历 session 时：

```go
for session := range sessions {
    sessionID := cfg.Sessions[session]
    if sessionID == "" {
        // 旧配置 fallback：用 --continue 取当前目录最近一次会话
        args = launcher.ResumeArgs("")  // 空 session_id → --continue
    } else {
        args = launcher.ResumeArgs(sessionID)
    }
    // tmux new-session -d -s <name> -c <dir> <agent> <args...>
}
```

### 2. ClaudeCodeLauncher.ResumeArgs 空值处理

`pkg/adapter/claude.go` — `ResumeArgs(sessionID)` 当 sessionID 为空时返回 `--continue` 而非 `--resume`：

```go
func (l *ClaudeCodeLauncher) ResumeArgs(sessionID string) []string {
    if sessionID == "" {
        return []string{"--continue", "--dangerously-skip-permissions"}
    }
    return []string{"--resume", sessionID, "--dangerously-skip-permissions"}
}
```

### 3. 提示用户

旧配置 resume 时打印提示，引导用户重新 init 以获得 session_id 记录：

```
⚠ session <name> 未记录 session_id，使用 --continue fallback
  建议重新执行 agentlink init 以启用精确恢复
```

只在 fallback 实际触发时打印，不干扰新配置用户。

### 4. 完全无 [sessions] 段的处理

如果 `cfg.Sessions` 整个为 nil（不是某个 session 缺失，是整个段都没有），`RunResume` 需要知道有哪些 session 要恢复。方案：

**从磁盘扫描**：读 `cfg.BaseDir` 下的子目录，每个含 `.agentlink.toml` 的目录就是一个 session。这和 `killSessionSessions` 的扫描逻辑一致（session.go:170），可复用。

```go
func listSessionsFromDisk(baseDir string) ([]string, error) {
    entries, err := os.ReadDir(baseDir)
    // 过滤含 .agentlink.toml 的目录
}
```

若 `cfg.Sessions` 为 nil，用 `listSessionsFromDisk` 得到 session 列表，全部走 `--continue` fallback。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/cli/resume.go` | fallback 分支 + 磁盘扫描 |
| `pkg/adapter/claude.go` | `ResumeArgs` 空值处理 |

## 不做

- 不自动迁移旧 config.toml（不主动写 `[sessions]` 段）。用户重新 init 才获得记录，保持 resume 只读 config。
- 不处理 `--continue` 恢复到错误会话的情况（Claude Code 的 `--continue` 取当前目录最近会话，若用户手动开过其他会话可能恢复错。提示已说明，不额外干预）。

## Acceptance criteria

- [ ] 旧 config.toml（无 `[sessions]`）执行 `agentlink resume` 不报错
- [ ] 旧配置 resume 时每个 session 用 `--continue` 启动
- [ ] 旧配置 resume 时打印 fallback 提示
- [ ] 新配置（有 `[sessions]`）resume 不受影响，仍用 `--resume <id>`
- [ ] `cfg.Sessions` 为 nil 时从磁盘扫描 session 列表
- [ ] `go test ./... -count=1` 全部通过
