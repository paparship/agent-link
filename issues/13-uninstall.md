# 13 — agentlink uninstall 命令

Type: AFK

## What to build

将现有的 `device remove` 重命名为 `uninstall`，并补全本地清理逻辑。

**当前 `device remove` 做的：**
- `DELETE /agents/device`（服务端注销，API Key 失效）
- 删除 `~/.agentlink/`

**缺少的：**
- 杀掉 tmux session（`main`、`worker`、`main-poller`、`worker-poller`）
- 删除 `base_dir`（默认 `agent_team/`）
- 可选：提示用户手动移除 PATH 配置（从 `~/.bashrc`）

**变更清单：**

1. `cmd/agentlink/main.go` — `device remove` → `uninstall`
   - 删除 `cmdDeviceRemove`，新增 `cmdUninstall`
   - 更新 `Usage` 和 help 文本
2. `pkg/cli/session.go` — `RunDeviceRemove` → `RunUninstall`
   - 新增：`exec.Command("tmux", "kill-session", "-t", "main").Run()` 等清理 tmux session
   - 新增：`os.RemoveAll(cfg.BaseDir)` 删除工作目录
3. `pkg/api/handlers.go` — 无需改动，`DELETE /agents/device` 复用
4. 更新 README 中的命令说明

**不做的：**
- 服务端数据恢复（一旦 uninstall，设备重新注册后从零开始）
- 不自动修改 `~/.bashrc`（只提示）

## Acceptance criteria

- [ ] `agentlink uninstall` 杀掉 `main`、`worker`、`main-poller`、`worker-poller` 四个 tmux session
- [ ] `agentlink uninstall` 调用 `DELETE /agents/device` 注销设备
- [ ] `agentlink uninstall` 删除 `~/.agentlink/`
- [ ] `agentlink uninstall` 删除 `base_dir`（如 `agent_team/`）
- [ ] `agentlink device remove` 不再存在
- [ ] 全部测试通过

## Blocked by

None — 可以立即开始
