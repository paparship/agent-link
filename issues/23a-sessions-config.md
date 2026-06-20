# 23a — config.toml 新增 [sessions] 段 + init 时记录 session_id

Type: AFK
Blocked by: 23（父 issue）

## 背景

`agentlink resume`（23b）需要知道每个 session 对应的 Claude Code `session_id`，才能精确恢复到 agentlink 管理的会话。当前 `~/.agentlink/config.toml` 不记录 session_id，resume 无法定位会话。

## 改动

### 1. AgentConfig 新增 Sessions 字段

`pkg/cli/config.go`：

```go
type AgentConfig struct {
    Server   string
    Device   string
    BaseDir  string
    Agent    string
    Poll     PollConfig
    Sessions map[string]string  // session_name → claude session_id
}
```

`loadConfig()` 解析 `[sessions]` 段。现有 `readTOML` 只支持单层 key，需要新增 `readTOMLSection(content, section string) map[string]string` 把 `[sessions]` 段下的所有 `key = "value"` 解析成 map。

### 2. writeConfigTOML 写入 [sessions] 段

`pkg/cli/init.go` — `writeConfigTOML` 签名扩展，接收 `sessions map[string]string`：

```toml
server = "http://..."
device = "..."
base_dir = "..."
agent = "claude"

[poll]
enabled = true
interval = 5

[sessions]
main = "01df3c38-..."
worker = "d3d3c1ab-..."
```

### 3. init 后读取 ~/.claude.json 获取 session_id

`pkg/cli/init.go` — `RunInit` 在创建 tmux session 后（`cmdInit` 中的 `exec.Command("tmux", "new-session", ...)` 之后），等待 Claude Code 写入 `~/.claude.json` 的 `lastSessionId`，然后读取并保存到 config.toml。

**新增函数：**

```go
// readClaudeSessionID reads ~/.claude.json and returns the lastSessionId.
// Returns "" if the file doesn't exist or the field is absent.
func readClaudeSessionID() (string, error)
```

`~/.claude.json` 是 JSON，结构含 `lastSessionId` 字段。Claude Code 启动后会写入此文件。

**写入时机问题：**

Claude Code 在 tmux session 中启动后，写入 `lastSessionId` 有延迟（需要进程完成初始化）。init 不能立即读取——需要轮询等待：

```go
// 等待最多 10s，每 500ms 检查一次 ~/.claude.json 的 lastSessionId 是否变化
```

问题：两个 session（main + worker）共享同一个 `lastSessionId` 字段，后启动的 worker 会覆盖 main 的值。需要**串行启动 + 立即读取**：

```
启动 main tmux session → 等 lastSessionId 更新 → 记录为 main 的 id
启动 worker tmux session → 等 lastSessionId 更新 → 记录为 worker 的 id
```

### 4. session add 同步记录

`pkg/cli/session.go` — `RunSessionAdd` 创建新 session 并启动 tmux 后，同样读取 `lastSessionId` 并更新 config.toml 的 `[sessions]` 段。

**新增函数：**

```go
// updateSessionID reads config.toml, updates the [sessions] entry for the given
// session name with a new session_id, and writes it back.
func updateSessionID(sessionName, sessionID string) error
```

### 5. tmux 启动逻辑从 main.go 下沉到 cli

当前 `cmdInit` 在 main.go 里直接调 `exec.Command("tmux", ...)`，init.go 的 `RunInit` 不管 tmux。这导致 session_id 记录点不统一。

重构：把 tmux session 创建逻辑移到 `pkg/cli/init.go` 的 `RunInit`（或新增 `launchSessions()` helper），main.go 只调 `RunInit`。这样 session_id 记录和 tmux 启动在同一个函数里，23b 的 resume 也能复用 `launchSessions`。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/cli/config.go` | `AgentConfig.Sessions` 字段 + `readTOMLSection` + `updateSessionID` |
| `pkg/cli/init.go` | `writeConfigTOML` 加 sessions 参数 + `readClaudeSessionID` + tmux 启动下沉 |
| `pkg/cli/session.go` | `RunSessionAdd` 调 `updateSessionID` |
| `cmd/agentlink/main.go` | `cmdInit` 移除 tmux 启动逻辑（下沉到 RunInit） |

## 不做

- 不改 `~/.claude.json` 的写入行为（只读）
- 不支持非 Claude Code agent 的 session_id 记录（adapter 接口暂不扩展，23 只针对 Claude Code）
- 不处理 Claude Code 未写入 `lastSessionId` 的极端情况（超时后记为空，23c fallback 处理）

## Acceptance criteria

- [ ] `init` 后 `~/.agentlink/config.toml` 包含 `[sessions]` 段，main 和 worker 各有 session_id
- [ ] `session add <name>` 后 config.toml 的 `[sessions]` 段新增对应条目
- [ ] tmux 启动逻辑从 main.go 移到 `pkg/cli/init.go`
- [ ] `loadConfig()` 能正确解析 `[sessions]` 段
- [ ] 旧 config.toml（无 `[sessions]` 段）`loadConfig` 不报错，`Sessions` 为 nil
- [ ] `go test ./... -count=1` 全部通过
