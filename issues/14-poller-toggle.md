# 14 — 设备级 poller 开关

Type: AFK
Blocked by: 12

## What to build

在 `config.toml` 中添加 `[poll]` 段，控制 poller 的启动行为。`init` 时加 `--no-poll` 跳过创建 poller session。

**config.toml 改动：**

```toml
server = "http://..."
device = "supermicro"
base_dir = "/path"
agent = "claude"

[poll]
enabled = true
interval = 5
```

`loadConfig()` 解析 `[poll]` 段，默认 `enabled = true`，`interval = 5`。

**init 流程改动（cmd/agentlink/main.go）：**
- 新增 `--no-poll` 参数
- 传 `--no-poll` 时，config.toml 写入 `[poll] enabled = false`
- 传 `--no-poll` 时，跳过创建 main-poller、worker-poller session

**RunPoll 改动（pkg/cli/poller.go）：**
- 如果 config 中 `poll.enabled = false`，直接退出不轮询
- 新增 `poll.interval` 替代硬编码的 5 秒间隔

## Acceptance criteria

- [ ] config.toml 支持 `[poll]` 段
- [ ] `agentlink init --no-poll` 不创建 poller session
- [ ] `agentlink init --no-poll` 写入 `[poll] enabled = false`
- [ ] `poll.enabled = false` 时 `agentlink poll` 直接退出
- [ ] `poll.interval` 覆盖默认 5s
- [ ] 不传 `--no-poll` 时行为不变（默认开）

## Blocked by

- Blocked by #12（12 做完后再做 14，因为 12 改了 poller 注入逻辑，14 基于此加开关）
