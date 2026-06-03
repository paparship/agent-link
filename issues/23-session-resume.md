# 23 — 重启恢复机制：session + poller resume

Type: Design

## 问题

服务器重启后，agentlink 的 tmux session、poller 进程、Claude Code 上下文全部丢失。用户需要重新 init（重新注册设备、重新创建配置），但元数据（config.toml、credentials.json、项目文件）都在磁盘上，理应可以恢复。

## 目标

一条命令 `agentlink resume` 重建所有运行时状态，不重新注册、不覆盖配置。

## 设计方案

### 1. 记录 session_id

init 创建 tmux session 启动 Claude Code 后，需要记下 Claude Code 生成的 session_id，以便 resume 时精确恢复到 agentlink 管理的会话（而不是用户手动开的其他会话）。

**写入时机：** init 创建 tmux session 后，等待 Claude Code 写入 `~/.claude.json` 的 `lastSessionId`，读取并保存。

**存储位置：** `~/.agentlink/config.toml` 新增 `[sessions]` 段：

```toml
server = "http://101.34.212.20:8080"
device = "jiefan-local"
base_dir = "/home3/jiefan/agentlink-test"
agent = "claude"

[poll]
enabled = true
interval = 5

[sessions]
main = "01df3c38-2236-4d34-88c8-62f87f2a5d18"
worker = "d3d3c1ab-db4b-4e40-99ea-10125beb8b8f"
```

### 2. agentlink resume 命令

```bash
agentlink resume
```

执行流程：

```
1. 读取 ~/.agentlink/config.toml → 获取 base_dir、device、server
2. 读取 [sessions] → 获取每个 session 的 session_id
3. 遍历每个 session（main、worker）：
   a. 读取 session 子目录的 .agentlink.toml 确认 session name
   b. tmux new-session -d -s <name> -c <dir> claude --resume <session_id> --dangerously-skip-permissions
   c. tmux new-session -d -s <name>-poller -c <dir> agentlink poll
4. 打印恢复结果
```

### 3. 调用链

```
机器重启
    ↓
systemd 自动拉起 agentlink server（已有）
    ↓
用户执行: agentlink resume
    ↓
读取已保存的 session_id → 精确恢复到 agentlink 管理的会话
    ↓
tmux main/worker + main-poller/worker-poller 就绪
```

### 4. 边界情况

- **没有 session_id 记录（旧版本升级）：** `--continue` 作为 fallback，取当前目录最近一次会话
- **session_id 对应的会话已被删除/过期：** `--continue` fallback
- **没有 [sessions] 段：** 打印提示 "未找到 session 记录，请执行 agentlink init"
- **设备已注销/不存在：** resume 失败，提示重新 init

## 子 issue

| 编号 | 范围 |
|------|------|
| 23a | config.toml 新增 [sessions] 段 + init 时记录 session_id |
| 23b | agentlink resume 命令实现（重建 tmux + poller） |
| 23c | 兼容旧配置（无 [sessions] 时用 --continue fallback） |

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/cli/config.go` | 新增 Sessions 字段（map[string]string） |
| `pkg/cli/init.go` | init 后读取 `~/.claude.json` 保存 session_id |
| `pkg/cli/session.go` | session add 时也记录 session_id |
| `cmd/agentlink/main.go` | 注册 `resume` 子命令 |
| `pkg/cli/resume.go` | **新增** — resume 命令实现 |
| `docs/deploy-server.md` | 补充重启后执行 resume 的说明 |

## Acceptance criteria

- [ ] init 后 `config.toml` 包含 `[sessions]` 段
- [ ] `agentlink resume` 重建 main/worker 的 tmux + poller
- [ ] `agentlink resume` 恢复的 Claude Code 是之前 agentlink 管理的会话（不是用户手动开的）
- [ ] 旧配置（无 [sessions]）resume 时用 --continue fallback 正常工作
- [ ] resume 后 `agentlink list --all` 设备在线
- [ ] resume 后消息收发正常
- [ ] resume 后 poller 心跳正常
