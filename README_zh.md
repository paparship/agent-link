# agentlink

跨设备 CLI 消息系统，用于多 Agent 协作。当前适配了 Claude Code。

## 部署服务端

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
agentlink send <target> <content>        # 发送
agentlink pull [--all]                    # 接收
```

### 任务

```bash
agentlink task send <target> <id> "<content>"    # 发放
agentlink task result <id> <status> "<result>"    # 回报
agentlink task resume <id> "<guidance>"           # 恢复
agentlink task cancel <id>                        # 取消
agentlink task status <id>                        # 查看
```

### 设备与 Session

```bash
agentlink ping                    # 心跳（标记在线）
agentlink list [--all]            # 查看设备
agentlink session add|remove <n>  # 管理 session
agentlink attach <session>        # 进入 session
agentlink device remove           # 注销设备
agentlink poll                    # 运行轮询（前台）
```

### 自动轮询

`init` 启动后每个 session 附带一个后台 poller：

- 每 5 秒轮询收件箱
- Agent 空闲时自动注入新消息
- Agent 忙时（生成中/用户输入中）跳过
- pane 捕获失败时静默跳过

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
