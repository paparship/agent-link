# 1 — Go 项目骨架 + API Server + Redis

Type: AFK
Blocked by: 无

## What to build

搭建 Go 项目基础结构和基础设施：

- Go module 初始化（`cmd/server/` + `cmd/agentlink/` + `pkg/`）
- `docker-compose.yml`（api server + redis）
- API server 骨架：HTTP 路由、启动、优雅关闭
- Redis 客户端连接
- 健康检查端点 `GET /health`

## Acceptance criteria

- [ ] `cmd/server/main.go` 启动 HTTP server
- [ ] `docker compose up` 同时启动 api 和 redis
- [ ] `curl /health` 返回 `{"ok": true}`
- [ ] Redis 客户端可连接（健康检查中验证）
- [ ] Go module 结构合理，`pkg/` 下存放共享代码

## Blocked by

None - can start immediately
