# 33 — 兼容 tclaude(腾讯内部 Claude Code wrapper)

Type: AFK

## What to build

支持用 `agentlink init --agent tclaude` 把 tclaude 作为底层 agent。tclaude 是腾讯内部的 Claude Code wrapper:除 `login/logout/update/daemon` 几个自有命令外,其余参数**原样转发给上游 Claude Code**,并通过设置 `CLAUDE_CONFIG_DIR=~/.tclaude` 让底层 claude 使用独立配置目录。

因此 tclaude 与 claude 的差异只有三处:
1. **可执行文件名**:`tclaude` 而非 `claude`
2. **会话文件位置**:`lastSessionId` 在 `~/.tclaude/.claude.json`(而非 `~/.claude.json`)
3. **认证前提**:使用前需 `tclaude login`(Tencent IOA)

命令语义、启动参数(`--continue`/`--resume`/`--dangerously-skip-permissions`)、TUI 均沿用 claude,可最大限度复用现有 `ClaudeCodeLauncher` 与 `ClaudeCodeDetector`。

## 现存缺口

`readClaudeSessionID`(`pkg/cli/runtime/init.go`)**硬编码读 `~/.claude.json`**。用 tclaude 时 `lastSessionId` 写在 `~/.tclaude/.claude.json`,agentlink 读不到 → 记录不到 session_id → `restart` 退化到 `--continue` fallback(无法精确 `--resume`)。这需要把"会话文件路径"抽象进 launcher。

## 改动清单

| # | 改动 | 文件 |
|---|------|------|
| 1 | `AgentLauncher` 接口新增 `SessionIDPath() string`,返回记录 `lastSessionId` 的 JSON 文件路径 | `pkg/adapter/adapter.go` |
| 2 | `ClaudeCodeLauncher` 实现 `SessionIDPath()` → `~/.claude.json` | `pkg/adapter/claude.go` |
| 3 | 新增 `TclaudeLauncher`,嵌入 `ClaudeCodeLauncher` 复用,覆盖 `Command()`(→tclaude)、`CheckPrereqs()`(→查 tclaude)、`SessionIDPath()`(→`~/.tclaude/.claude.json`) | `pkg/adapter/tclaude.go`(新) |
| 4 | `NewLauncher` / `NewDetector` 增加 `"tclaude"` 分支;detector 直接复用 `ClaudeCodeDetector`(TUI 相同) | `pkg/adapter/adapter.go` |
| 5 | `readClaudeSessionID` / `readClaudeSessionIDWithTimeout` 改为接收路径参数,调用点传 `launcher.SessionIDPath()`,去掉 `~/.claude.json` 硬编码 | `pkg/cli/runtime/init.go` |

选择方式:`agentlink init --agent tclaude ...`;默认仍为 `claude`。

## 前提与风险

- **认证**:用 tclaude 前需 `tclaude login`,否则底层 claude 无法启动。未登录时的启动失败会体现在 issue 32 的启动日志中。
- **TUI 检测(需运行验证)**:poller 依赖 `"esc to interrupt"`(busy)与 `"❯"`(prompt)判断可否注入消息。tclaude 为纯转发,理论上 TUI 与 claude 一致,故复用 `ClaudeCodeDetector`;但若 tclaude 包装了额外 UI 遮挡这些标志,消息注入会失效——需实际运行 `tclaude` 检查 pane 内容确认。

## 影响范围

纯 CLI + adapter 层,不涉及 server。

## Acceptance criteria

- [ ] `AgentLauncher` 接口含 `SessionIDPath() string`,`ClaudeCodeLauncher` 与 `TclaudeLauncher` 均实现
- [ ] `NewLauncher("tclaude")` 返回 `TclaudeLauncher`,`Command()` 为 `tclaude ...`
- [ ] `NewDetector("tclaude")` 返回可用的 detector(复用 `ClaudeCodeDetector`)
- [ ] `TclaudeLauncher.SessionIDPath()` 指向 `~/.tclaude/.claude.json`;`ClaudeCodeLauncher.SessionIDPath()` 指向 `~/.claude.json`
- [ ] `readClaudeSessionID` 不再硬编码 `~/.claude.json`,而是使用 `launcher.SessionIDPath()`
- [ ] `agentlink init --agent tclaude` 可注册并拉起 tclaude 会话
- [ ] 默认(`--agent` 缺省)行为仍为 claude,现有测试通过
- [ ] 未引入新的第三方依赖
