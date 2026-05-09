# 19 — 服务端卸载/清理

Type: AFK

## 背景

当前没有服务端卸载的文档或命令。需要清理时（如重装、迁移），没有清晰的步骤。

## 服务端残留清单

### 需要清理的

| 资源 | 位置 | 说明 |
|------|------|------|
| server binary | `deploy/server` | 直接删除 |
| server log | `deploy/server.log` | 直接删除 |
| 旧 CLI binary | `deploy/agentlink` | 服务端用不到，可删 |
| PID 文件 | `/tmp/agentlink-server.pid` | stop.sh 会自动清理 |
| Redis 数据 | `redis-cli FLUSHALL` | 或用 pattern 删除 `device:*` `api_key:*` `inbox:*` `task:*` `tasks:*` |

### 不应清理的

- `start.sh` / `stop.sh` — 重装需要
- `agent-link-server.service` — 保留
- Redis 本身 — 独立服务，不卸载
- 源码目录 `/home/jiefan/agent-link/cmd/` 等 — CI 要用

## 完整卸载步骤

```bash
# 1. 停服务
cd /home/jiefan/agent-link/deploy && bash stop.sh

# 2. 清 Redis 数据
redis-cli FLUSHALL

# 3. 删文件
rm -f deploy/server deploy/server.log deploy/agentlink

# 4. 可选：删整个项目目录
rm -rf /home/jiefan/agent-link
```

## 重新部署

部署新版本时只需覆盖 `deploy/server` 然后 restart，无需清理 Redis。

## Acceptance criteria

- [ ] 按步骤执行后服务端恢复到未部署状态
- [ ] 重新部署后能正常 init 注册

## Blocked by

- Blocked by #19
