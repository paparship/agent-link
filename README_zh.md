# agentlink

跨设备 CLI 消息系统，用于多 Agent 协作。当前适配了 Claude Code。

## 一键安装（Linux / macOS）

```bash
curl -sfL https://github.com/paparship/agent-link/releases/latest/download/install.sh | sh
```

## 编译

```bash
make build              # 编译 agentlink CLI
make build-server       # 编译 server
make install            # 编译 + 安装到 /usr/local/bin
make uninstall          # 从 /usr/local/bin 移除
make reinstall          # 卸载 → 编译 → 安装
make test               # 运行全部测试
make clean              # 删除编译产物
```

通过 `BINDIR` 自定义安装路径：

```bash
make install BINDIR=~/.local/bin
```

## 部署服务端

完整 systemd 部署指南见 [docs/deploy-server.md](docs/deploy-server.md)。

依赖 Redis，通过环境变量配置：

```bash
export REDIS_ADDR=localhost:6379
export REGISTER_PASSWORD=<设置注册密码>
export LISTEN_ADDR=:8080

./server
```

### 注册流程

1. 部署 server
2. 任意设备执行 `init` 注册（需 server URL + 注册密码）
3. Server 自动分配 API key，设备上线

## CLI 使用

### 初始化团队

```bash
agentlink init --server http://<server>:8080 --password <password> [./path]
```

创建 `main/` 和 `worker/` 目录，各含 `.agentlink.toml` + `CLAUDE.md`。自动启动两个 tmux session（`main`、`worker`）运行已配置的 agent（默认为 Claude Code），每个 session 附带一个后台轮询进程。

### 消息

```bash
agentlink send [--interrupt] <target> <content>   # 发送（--interrupt 中断忙碌的 agent）
agentlink pull [--all]                             # 接收
agentlink message status <id>                      # 查询投递状态
```

`send` 会输出接收方状态面板：空闲 / 忙碌（含当前 task 及已进行时长） / 离线，以及未读消息数。成功投递后显示消息 ID，可用于后续状态查询。

### 任务

```bash
agentlink task send [--interrupt] <target> [<task_id>] "<content>"   # 发放
agentlink task result <task_id> <status> "<result>"                   # 回报
agentlink task resume <task_id> "<guidance>"                          # 恢复
agentlink task cancel <task_id>                                       # 取消
agentlink task status <task_id>                                       # 查看
agentlink task list                                                   # 列出本设备任务
```

### 设备与 Session

```bash
agentlink ping                    # 心跳（标记在线）
agentlink list [--all]            # 查看设备
agentlink session add|remove <n>  # 管理 session
agentlink attach <session>        # 进入 session
agentlink resume                  # 重启后恢复 tmux + poller
agentlink uninstall               # 注销设备 + 清理本地文件
agentlink poll                    # 运行轮询（前台）
```

### 自动轮询

`init` 启动后每个 session 附带一个后台 poller：

- 每 5 秒轮询收件箱
- Agent 空闲时自动注入新消息
- 注入的消息带 `[来自 device:session 的消息]` 标识头，便于 agent 区分队列注入与用户输入
- 每 ~60 秒发送心跳，保持设备在线状态
- 首次启动时自动接受 Claude Code trust prompt
- Agent 忙时（生成中 / 用户输入中）跳过
- pane 捕获失败时静默跳过

### 重启恢复

`agentlink resume` 从磁盘配置重建 tmux session 和 poller，不重新注册设备。机器重启后执行：

```bash
agentlink resume
```

每个 session 的 Claude Code 会恢复到上次记录的 `session_id`（保存在 `~/.agentlink/config.toml` 的 `[sessions]` 段）。对于该功能上线前的旧配置，自动用 `--continue` 作为 fallback。

## 数据保留

Redis 数据有 TTL，防止无限堆积：

| 数据 | 保留时长 |
|------|----------|
| 收件箱未读消息 | 7 天 |
| 已投递消息记录 | 24 小时 |
| 已完成任务记录 | 30 天 |

## 架构

```
┌───────────┐     ┌───────────────┐     ┌─────────┐
│  客户端   │────▶│   API 服务端  │────▶│  Redis  │
└───────────┘     └───────────────┘     └─────────┘
                       │
                 ┌─────┴─────┐
                 │   tmux    │
                 └───────────┘
```

- **服务端**: Go net/http + Redis（消息队列、任务存储、设备注册）
- **客户端**: 通过 HTTP API 收发消息，通过 tmux 与 agent 交互
- **轮询器**: 后台检测收件箱，agent 空闲时自动注入消息
- **认证**: API key（SHA256 索引） + Bearer Token
