# agentlink

跨设备 CLI 消息系统，用于多 Agent Claude Code 协作。

## 部署 Server

依赖 Redis，通过环境变量配置：

```bash
export REDIS_ADDR=localhost:6379
export REGISTER_PASSWORD=<设置注册密码>
export LISTEN_ADDR=:8080

./server
```

### 注册流程

1. 部署 server
2. 用 `init` 命令注册设备（需提供 server URL 和注册密码）
3. Server 自动分配 API key，设备上线

## CLI 使用

### 初始化

```bash
agentlink init --server http://<server>:8080 --password <password> [./path]
```

- 创建 `main/` 和 `worker/` 目录，各含 `.agentlink.toml` + `CLAUDE.md`
- 自动启动两个 tmux session（`main`、`worker`）运行 Claude Code
- 每个 session 附带一个后台 poller 进程（`main-poller`、`worker-poller`）

### 消息

```bash
agentlink send <target> <content>        # 发送消息
agentlink pull [--all]                    # 拉取收件箱
```

### 任务

```bash
agentlink task send <target> <id> "<content>"    # 发放任务
agentlink task result <id> <status> "<result>"    # 回报结果
agentlink task resume <id> "<guidance>"           # 恢复挂起
agentlink task cancel <id>                        # 取消任务
agentlink task status <id>                        # 查看状态
```

### 其他

```bash
agentlink ping                    # 心跳（标记在线）
agentlink list [--all]            # 查看设备状态
agentlink session add|remove <n>  # 管理 session
agentlink attach <session>        # 进入 session
agentlink device remove           # 注销设备
agentlink poll                    # 后台轮询收件箱（前台运行）
```

### 自动轮询

`init` 启动后，每个 session 的 poller 自动运行：

- 每 5 秒拉取收件箱
- 检测 Claude 空闲时自动注入新消息
- Claude 忙时（生成中 / 用户输入中）跳过
- pane 捕获失败时静默跳过

## 架构

```
┌───────────┐     ┌──────────────┐     ┌─────────┐
│  CLI 客户端 │────▶│  Go API Server │────▶│  Redis  │
└───────────┘     └──────────────┘     └─────────┘
                        │
                   ┌────┴────┐
                   │  tmux   │ (capture-pane / send-keys)
                   └─────────┘
```

- **Server**: Go net/http + Redis（消息队列、任务存储、设备注册）
- **CLI**: 通过 HTTP API 收发消息，通过 tmux 与 Claude 交互
- **Poller**: 后台轮询，检测 Claude 空闲后自动注入消息
- **认证**: API key（SHA256 索引） + Bearer Token
