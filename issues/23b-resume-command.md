# 23b — agentlink resume 命令实现（重建 tmux + poller）

Type: AFK
Blocked by: 23a

## 背景

机器重启后，tmux session、poller 进程、Claude Code 上下文全部丢失，但磁盘上的 config.toml、credentials.json、项目文件都还在。用户被迫重新 `init`（重新注册设备、覆盖配置）。23a 已记录每个 session 的 `session_id`，本 issue 用它实现 `agentlink resume`。

## 改动

### 1. 新增 pkg/cli/resume.go

```go
func RunResume() error
```

执行流程：

```
1. loadConfig() → 获取 base_dir、device、agent、[sessions]
2. loadCredentials() → 获取 api_key
3. 遍历 config.Sessions（或固定 main/worker）：
   a. 读 <base_dir>/<session>/.agentlink.toml 确认 session name
   b. 若 config.Sessions[session] 非空：
      tmux new-session -d -s <name> -c <dir> claude --resume <session_id> --dangerously-skip-permissions
   c. 若为空（旧配置，23c 处理）：
      tmux new-session -d -s <name> -c <dir> claude --continue --dangerously-skip-permissions
   d. 若 poll.enabled：
      tmux new-session -d -s <name>-poller -c <dir> <self_exe> poll
4. 发一次心跳（RunPing）让设备立即上线
5. 打印恢复结果
```

**关键：复用 23a 下沉到 init.go 的 `launchSessions` helper。** resume 和 init 的唯一区别是 tmux 启动参数（`--resume <id>` vs 无 flag），抽一个带选项的函数：

```go
type launchOpts struct {
    Resume     bool              // true = --resume <session_id>, false = fresh
    Sessions   map[string]string // 23a 记录的 session_id
    NoPoll     bool
}

func launchSessions(baseDir, agent string, opts launchOpts) error
```

`RunInit` 调 `launchSessions(baseDir, agent, launchOpts{Resume: false, ...})`，`RunResume` 调 `launchSessions(baseDir, agent, launchOpts{Resume: true, Sessions: cfg.Sessions, ...})`。

### 2. claude adapter Command() 扩展

`pkg/adapter/claude.go` — `ClaudeCodeLauncher.Command()` 当前返回 `("claude", [])`。resume 需要传 `--resume <id> --dangerously-skip-permissions`。

两种方案：
- **方案 A**：`Command()` 不变，新增 `CommandWithResume(sessionID string) (string, []string)`。简单但接口膨胀。
- **方案 B**：`AgentLauncher` 接口加 `ResumeArgs(sessionID string) []string` 方法，返回 resume 专用参数。`launchSessions` 根据 opts.Resume 决定调 `Command()` 还是 `ResumeArgs()`。

选方案 B，保持接口对称：

```go
type AgentLauncher interface {
    Command() (name string, args []string)
    ResumeArgs(sessionID string) []string  // 新增
    CheckPrereqs() error
    InitTemplate(session, device string) string
}
```

ClaudeCode 实现：

```go
func (l *ClaudeCodeLauncher) ResumeArgs(sessionID string) []string {
    return []string{"--resume", sessionID, "--dangerously-skip-permissions"}
}
```

### 3. cmd/agentlink/main.go 注册 resume 子命令

```go
case "resume":
    cmdResume(os.Args[2:])
```

```go
func cmdResume(args []string) {
    if err := cli.RunResume(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %s\n", err)
        os.Exit(1)
    }
}
```

`printUsage` 加一行：

```
agentlink resume
```

### 4. 不重新注册

`RunResume` **不调** `/agents/register`。设备已在 Redis 中（`agentlink:device:<name>` 存在），只发心跳让 `last_seen` 刷新即可。若设备不存在（已注销），resume 失败提示重新 init。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/cli/resume.go` | **新增** — `RunResume` |
| `pkg/cli/init.go` | 抽出 `launchSessions` helper |
| `pkg/adapter/adapter.go` | `AgentLauncher` 接口加 `ResumeArgs` |
| `pkg/adapter/claude.go` | `ClaudeCodeLauncher.ResumeArgs` 实现 |
| `cmd/agentlink/main.go` | 注册 `resume` 子命令 + printUsage |

## 不做

- 不自动 resume（不做 systemd unit 的 `ExecStartPost`）。resume 是显式命令，用户决定何时恢复。
- 不恢复 poller 的"上次处理到哪条消息"状态（poller 无状态，重启即重新轮询 inbox）。
- 不处理 tmux session 已存在的情况（先 kill 再 new，和 init 一致）。

## Acceptance criteria

- [ ] `agentlink resume` 重建 config.Sessions 中所有 session 的 tmux + poller
- [ ] 恢复的 Claude Code 带 `--resume <session_id>` 参数
- [ ] resume 后 `agentlink list --all` 显示本设备在线
- [ ] resume 后消息收发正常（send + pull）
- [ ] resume 后 poller 心跳正常
- [ ] resume 不重新注册设备（不调 /agents/register）
- [ ] 设备已注销时 resume 失败并提示重新 init
- [ ] `go test ./... -count=1` 全部通过
