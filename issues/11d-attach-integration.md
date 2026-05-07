# 11d — Attach 集成 Adapter

Type: AFK
Blocked by: 11a, 11b

## What to build

把 `AgentLauncher` 接入 `RunAttach`，替代硬编码的 `"claude"`。

**RunAttach 改动：**
- 从 config 读 `agent` 字段（当前只读 server/device/base_dir，需要加 agent）
- 实际上 `loadConfig()` 已在 11b 中加了 `Agent` 字段
- `launcher.Command()` 返回 binary + args 用于 `exec.Command`

**Remove hardcoded claude launch：**
- `session.go:175` — `exec.Command("tmux", "new-session", ..., "claude", "--dangerously-skip-permissions")`
  改为 `name, args := launcher.Command(); exec.Command("tmux", append([]string{"new-session", ..., name}, args...)...)`

**claudeMDContent 已删除**（11b 中移至 adapter），这里不涉及。

**测试改动：**
- `TestRunAttach_errors` 保持不变（错误路径不涉及 launcher）
- 无需测试 attach 成功路径（仍需要真实 tmux）

## Acceptance criteria

- [ ] `RunAttach` 通过 `loadConfig()` 的 `Agent` 字段获取 launcher
- [ ] `RunAttach` 使用 `launcher.Command()` 替代硬编码 `"claude"`
- [ ] 不传 `--agent` 时默认 `"claude"`（config 中已有默认值）
- [ ] `TestRunAttach_errors` 通过

## Blocked by

- Blocked by #11a, #11b
