# 11 — Agent 抽象层：从 Claude Code 绑定解耦为通用 agent 网络层

Type: HITL

## 背景

目前 agentlink 在 10 处硬编码了 Claude Code 的细节（binary 名、TUI 标志、配置文件）。
产品方向不是 Claude Code 的插件，而是所有 CLI agent 的网络层。

## 设计决策（grill-with-docs 结论）

```
pkg/adapter/
├── adapter.go     # AgentLauncher, IdleDetector 接口 + 注册函数
├── claude.go      # ClaudeCode 实现
└── claude_test.go
```

**AgentLauncher** — 怎么启动 agent（binary 名 + 参数 + 预检 + 初始化模板）

```go
type AgentLauncher interface {
    Command() (name string, args []string)
    CheckPrereqs() error
    InitTemplate(session string) string  // CLAUDE.md 等提示词内容
}
```

**IdleDetector** — 判断 agent 是否空闲

```go
type IdleDetector interface {
    IsBusy(paneContent string) bool       // 是否在生成/跑工具
    IsPromptEmpty(paneContent string) bool // 输入框是否为空
}
```

**注册方式**: `switch/case` 查找（简单够用，有第二个 adapter 再考虑注册表）

```go
func NewLauncher(name string) (AgentLauncher, error)
func NewDetector(name string) (IdleDetector, error)
```

**配置**: `config.toml` 加 `agent = "claude"`，init flag 写入，后续 poll/attach 读取。
`InitOptions` 加 `Agent string` 字段，`RunInit` 内部通过 launcher 获取命令。

**Poller**: `IdleDetector` 作为字段注入，不再直接调 `IsClaudeIdle`。

## 接口定义详情

```go
package adapter

type AgentLauncher interface {
    // Command 返回启动 agent 的 binary 和参数（不含 tmux new-session 部分）
    Command() (name string, args []string)
    // CheckPrereqs 验证 agent 所需前置条件（binary 是否在 PATH 等）
    CheckPrereqs() error
    // InitTemplate 返回初始化提示词模板（写入 CLAUDE.md 等文件）
    InitTemplate(session string) string
}

type IdleDetector interface {
    // IsBusy 判断 agent 是否正在生成/运行（检测状态栏等标志）
    IsBusy(paneContent string) bool
    // IsPromptEmpty 判断 agent 输入框是否为空
    IsPromptEmpty(paneContent string) bool
}
```

## 现有绑定清单

| # | 位置 | 耦合内容 | 目标 |
|---|---|---|---|
| 1 | main.go:109 | `exec.Command("claude", ...)` | → `launcher.Command()` |
| 2 | session.go:175 | `exec.Command(..., "claude", ...)` | → `launcher.Command()` |
| 3 | init.go:125 | checkPrereqs 查 `"claude"` | → `launcher.CheckPrereqs()` |
| 4 | poller.go:24 | IsClaudeIdle 搜 `esc to interrupt` / `❯` | → `detector.IsBusy()` + `IsPromptEmpty()` |
| 5 | session.go:182 | claudeMDContent() | → `launcher.InitTemplate()` |
| 6 | init.go:107-108 | 写 CLAUDE.md | → 文件名可配置 |
| 7 | session.go:57-60 | 写 CLAUDE.md | → 文件名可配置 |

## Acceptance criteria

- [ ] `pkg/adapter/` 包，包含 `AgentLauncher` + `IdleDetector` 接口
- [ ] `ClaudeCodeLauncher` 实现（Command / CheckPrereqs / InitTemplate）
- [ ] `ClaudeCodeDetector` 实现（IsBusy / IsPromptEmpty）
- [ ] `config.toml` 支持 `agent` 字段，默认 `"claude"`
- [ ] `init --agent claude` 写入 config，init 流程使用 launcher
- [ ] Poller 通过注入的 detector 判断空闲，不再直接调 IsClaudeIdle
- [ ] attach 通过 config 读 agent 类型，使用对应 launcher
- [ ] 所有原有测试依然通过
- [ ] agentlink-core 不引用任何 Claude Code 常量

## Blocked by

- Blocked by #10
