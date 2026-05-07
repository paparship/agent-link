# 11a — Adapter 接口定义 + ClaudeCode 实现

Type: AFK
Blocked by: 10, 11

## What to build

创建 `pkg/adapter/` 包，定义 `AgentLauncher` 和 `IdleDetector` 接口，并提供 Claude Code 实现。

```go
type AgentLauncher interface {
    Command() (name string, args []string)
    CheckPrereqs() error
    InitTemplate(session string) string
}

type IdleDetector interface {
    IsBusy(paneContent string) bool
    IsPromptEmpty(paneContent string) bool
}
```

**ClaudeCodeLauncher：**
- `Command()` → `("claude", []string{"--dangerously-skip-permissions"})`
- `CheckPrereqs()` → 从 `checkPrereqs` 搬过来（查 claude + tmux 在 PATH）
- `InitTemplate(session)` → 从 `claudeMDContent(session)` 搬过来

**ClaudeCodeDetector：**
- `IsBusy(pane)` → `strings.Contains(pane, "esc to interrupt")`
- `IsPromptEmpty(pane)` → 从底部扫最后一行含 `❯` 的，检查后面是否只有空白

**注册方式：** `NewLauncher("claude")` / `NewDetector("claude")`，switch/case 查找。

## Acceptance criteria

- [ ] `pkg/adapter/adapter.go` — 两个接口 + 注册函数
- [ ] `pkg/adapter/claude.go` — 两个 Claude Code 实现
- [ ] `pkg/adapter/claude_test.go` — 覆盖 IsBusy/IsPromptEmpty
- [ ] `go build ./...` 通过
- [ ] 不修改 pkg/cli/ 和 pkg/api/ 的任何现有代码

## Blocked by

- Blocked by #10, #11
