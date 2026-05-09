# 20 — 服务端卸载/清理

Type: AFK

## 背景

当前没有服务端卸载的文档。需要清理时（如重装、迁移），没有清晰的步骤。

## 服务端残留清单

### 需要清理的

| 资源 | 位置 | 说明 |
|------|------|------|
| server binary | `deploy/server` | 9.5 MB |
| server log | `deploy/server.log` | 一般很小 |
| 旧 CLI binary | `deploy/agentlink` | 服务端用不到 |
| PID 文件 | `/tmp/agentlink-server.pid` | stop.sh 会自动清理 |
| Redis 数据 | `redis-cli DEL agentlink:*` | 精准删除，不影响 Redis 上其他服务 |

### 不应清理的

- `start.sh` / `stop.sh` — 部署脚本，重装需要
- `agent-link-server.service` / `redis.service` — systemd 配置，保留不影响
- Redis 本身 — 独立服务，不要卸载
- `/home/jiefan/agent-link/`目录下的源码 — CI 要用

## 完整卸载步骤

```bash
# 1. 停服务（自动清理 PID 文件）
cd /home/jiefan/agent-link/deploy && bash stop.sh

# 2. 精准删除 agentlink 的 Redis 数据（不影响其他服务）
redis-cli KEYS "agentlink:*" | xargs -r redis-cli DEL

# 3. 删文件
rm -f /home/jiefan/agent-link/deploy/server
rm -f /home/jiefan/agent-link/deploy/server.log
rm -f /home/jiefan/agent-link/deploy/agentlink

# 4. 可选：删整个项目目录（如需彻底移除）
rm -rf /home/jiefan/agent-link
```

## 重新部署

部署新版本时只需覆盖 `deploy/server` 然后 restart，无需清理 Redis。

## Acceptance criteria

- [ ] 按步骤执行后服务端恢复到未部署状态（端口无监听、Redis 无 agentlink key）
- [ ] 重新部署后能正常 init 注册

## Blocked by

- Blocked by #19（Redis key 前缀改为 `agentlink:` 后才能用精准删除）
