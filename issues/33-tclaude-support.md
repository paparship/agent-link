# 33 — 兼容 tclaude(腾讯内部 Claude Code wrapper)

Type: AFK

## What to build

支持用 `agentlink init --agent tclaude` 把 tclaude 作为底层 agent。tclaude 是腾讯内部的 Claude Code wrapper:除 `login/logout/update/daemon` 几个自有命令外,其余参数**原样转发给上游 Claude Code**,并通过设置 `CLAUDE_CONFIG_DIR=~/.tclaude` 让底层 claude 使用独立配置目录(`~/.tclaude/.claude.json`、`~/.tclaude/projects/…`)。

因为除二进制名外行为与 claude 一致,兼容成本很低:最大化复用现有 `ClaudeCodeLauncher` / `ClaudeCodeDetector`。

## 实现

- 新增 `TclaudeLauncher`,**嵌入 `ClaudeCodeLauncher`** 复用 `ResumeArgs` / `InitTemplate`,仅覆盖:
  - `Command()` → 二进制名 `tclaude` + `--dangerously-skip-permissions`
  - `CheckPrereqs()` → 检查 `tmux` + `tclaude`
- `NewLauncher` 增加 `tclaude` 分支;`NewDetector` 让 `claude`/`tclaude` 共用 `ClaudeCodeDetector`(TUI 相同,busy / prompt 标记一致)。
- 单元测试覆盖 Command 名/参数、继承的 ResumeArgs、NewDetector。

## 前提

用 tclaude 前需先 `tclaude login`(腾讯 IOA 登录)。消息注入依赖 TUI 的 `esc to interrupt` / `❯` 标记——tclaude 纯转发不改 TUI,故复用 claude 的检测逻辑。

## 备注

> session_id 的记录/恢复**不在本 issue**。原先设想的"为 tclaude 抽象 `SessionIDPath()` 以回读 `lastSessionId`"经源码核对后确认不可行(`lastSessionId` 仅在退出时落盘),已统一归入 **#34**:改为 agentlink 自生成 `--session-id` + restart 用 `--continue`。因此本 issue 不引入 `SessionIDPath`,claude 与 tclaude 的 session id 处理由 #34 统一负责。
