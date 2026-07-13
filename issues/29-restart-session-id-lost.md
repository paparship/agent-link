# 29 — restart session_id 丢失 + 不保存

Type: BUG
Status: CLOSED — 与 #30 同源(都卡在读不到 Claude 的 lastSessionId)。经源码核对,本文 Bug 29B 的根因分析不准确(见下方修正说明),且"启动后回读 lastSessionId"这一路线从根上不可行。统一由 **#34** 给出完整方案。

> **根因修正(源码核对 Claude Code v2.1.197 后)**:`lastSessionId` 并非"迁移到独立文件",而是 (a) 存在 `~/.claude.json` 的 `projects[<cwd>]` 下、而非顶层;(b) **仅在 claude 退出 / 切换会话时写盘**,会话运行期间不更新。因此 `init`/`restart` 启动 claude 后等 10s 回读,必然读到*上一次*会话的旧 id 或空值——这才是 29B 的真正病根。详见 #34。

## 现象

1. `agentlink restart` 每个 session 都显示 `(session_id unavailable, continue fallback)`
2. 运行后 config.toml 的 `[sessions]` 里 session_id 永远是空字符串
3. 每次 restart 都用 `--continue` fallback,无法精确 `--resume` 对话

## 根因

### Bug A: restart 丢弃录到的 session_id

`resume.go:52` 调用 `launchSessions` 后,返回值是 `map[string]string`(session_id 映射),但被 `_` 丢弃:

```go
if _, err := launchSessions(cfg.BaseDir, cfg.Agent, launchOpts{...}); err != nil {
```

即使 `readClaudeSessionIDWithTimeout` 成功捕获了 session_id,也不会写回 config。下次 restart 时 config 仍然是 `""`。

**对比**:`init.go` 在调用 `launchSessions` 后正确写入了 config。

### Bug B: readClaudeSessionID 读取方式可能已失效

`init.go` 的 `readClaudeSessionID` 读取 `~/.claude.json` 顶层的 `lastSessionId` 字段。
当前环境 `~/.claude.json` 里没有这个字段。Claude Code 可能已将 session 存储迁移到 `~/.claude/projects/<project-dir>/` 下的独立文件。
10 秒超时后返回 `""`,config 写入空值。

## 验证方法

- 检查 `~/.claude.json` 的完整结构,确认 `lastSessionId` 字段是否存在
- 检查 `~/.claude/projects/` 下是否有 session_id 信息
- 跟踪 Claude Code 启动后何时、如何写入 session_id 到磁盘
