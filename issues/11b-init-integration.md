# 11b — Init 集成 Adapter

Type: AFK
Blocked by: 11a

## What to build

把 adapter 接入 init 流程：

**config.toml 改动：**
- `AgentConfig` 加 `Agent` 字段，`loadConfig()` 读取 `agent` 值
- 默认 `"claude"`，不传 `--agent` 时行为不变

**InitOptions / RunInit 改动：**
- `InitOptions` 加 `Agent string`
- `checkPrereqs()` 调用 → `launcher.CheckPrereqs()`
- CLAUDE.md 生成 → `launcher.InitTemplate(session)`
- tmux session 启动 → `launcher.Command()` 返回 binary + args
- config 写入包含 `agent` 字段

**cmd/agentlink/main.go 改动：**
- `cmdInit` 加 `--agent` flag，默认 `"claude"`

**删除旧函数：**
- `checkPrereqs()` 从 init.go 删除（逻辑在 adapter 内）
- `claudeMDContent()` 从 session.go 删除（逻辑在 adapter 内）

## Acceptance criteria

- [ ] `init --agent claude` 写入 config.toml
- [ ] 不传 `--agent` 时默认 `"claude"`
- [ ] `RunInit` 通过 `NewLauncher(cfg.Agent)` 获取 launcher
- [ ] tmux session 使用 `launcher.Command()` 启动
- [ ] CLAUDE.md 使用 `launcher.InitTemplate()` 生成
- [ ] `checkPrereqs()` 从 init.go 删除
- [ ] `claudeMDContent()` 从 session.go 删除
- [ ] 所有原有 init 测试通过

## Blocked by

- Blocked by #11a
