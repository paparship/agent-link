# 6 — session 管理 + attach

Type: AFK
Blocked by: 2

## What to build

Session 的增删和管理，以及 tmux session 的 attach 封装。

服务端：
- `PATCH /agents/sessions`：向当前设备追加或移除 session
- `DELETE /agents/sessions?name=<session>`：移除指定 session
- `DELETE /agents/device`：注销整个设备（清理 inbox、任务、API Key 失效）

CLI：
- `agentlink session add <name>` — 创建目录、.agentlink.toml、CLAUDE.md、API 注册
- `agentlink session remove <name>` — 移除 session
- `agentlink device remove` — 注销本设备
- `agentlink attach <session>` — tmux 封装：
  - tmux session 已存在 → `tmux attach -t <session>`
  - 不存在 → `tmux new-session -c {base_dir}/<session> 'claude'`

## Acceptance criteria

- [ ] `agentlink session add reviewer` → 创建目录、.agentlink.toml、CLAUDE.md
- [ ] `agentlink attach main` 进入已存在的 tmux session
- [ ] `agentlink attach reviewer` 自动创建 tmux session 并进入
- [ ] `agentlink session remove reviewer` 移除 session（服务端 + 本地）
- [ ] `agentlink device remove` 注销设备
- [ ] 提醒输出：session add 后提示 `agentlink attach <name>` 进入

## Blocked by

- Blocked by #2
