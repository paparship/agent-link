# 18 — `init` 幂等：同名设备重装时复用已有 API key

Type: AFK

## 背景

当前 `init` 遇到同名设备时返回 409 Conflict。这意味着卸载重装时必须换设备名或手动清 Redis，流程断裂。

用户预期是：在同一台设备上卸载后重装，应能直接用同一设备名重新 init，无需额外操作。就像手机 App 卸载重装后，账号还在。

## 设计方案

不引入账号层。服务端通过 **device name + register_password** 校验来识别是否同一设备。

### 服务端改动（`handleRegister`）

```
POST /agents/register
  device: supermicro
  register_password: agentlink123
```

当前逻辑：
- 设备不存在 → 注册 → 201
- 设备已存在 → 409

改为：
- 设备不存在 → 注册 → 200
- 设备已存在 + 正确 password → 返回已有 API key（幂等，200）
- 设备已存在 + 正确 password + `--force` → 删除旧设备，重新注册（200）
- 设备已存在 + 错误 password → 401
  - 响应提示："use --force to override"

### 客户端改动（`RunInit`）

- 服务端返回 200 + 已有 API key → 本地正常写入 config + CLAUDE.md（覆盖旧目录）
- `init` 不因设备已存在而失败

### 客户端 `uninstall` 调整

当前 `uninstall` 同时做本地清理 + `DELETE /agents/device`。改为分离：

| 命令 | 做什么 |
|------|--------|
| `agentlink uninstall` | 仅本地清理（杀 tmux、删配置、删工作目录） |
| `agentlink uninstall --purge` | 本地清理 + 服务端注销（调 DELETE /agents/device，即当前 `uninstall` 行为） |

**涉及：** `cmd/agentlink/main.go` — 新增 `--purge` 参数；`pkg/cli/session.go` — `RunUninstall` 新增 `purge` 参数，控制是否调 `DELETE /agents/device`。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | `handleRegister` — 设备已存在时校验 password 而非 409 |
| `pkg/api/handlers_test.go` | 更新注册测试 |
| `cmd/agentlink/main.go` | `cmdUninstall` 新增 `--purge` 参数 |
| `pkg/cli/session.go` | `RunUninstall` 新增 `purge` 参数，控制调 `DELETE /agents/device` |

## Redis 数据类型

| key 模式 | 内容 | 丢失影响 |
|----------|------|---------|
| `device:` | 设备注册信息（session 列表、API key hash） | 重新 init 即可恢复 |
| `api_key:` | API key → 设备名反向索引 | 重新 init 即可恢复 |
| `inbox:` | 待拉取的消息队列（RPop 即删） | 最多丢几条未读消息 |
| `task:` | 任务详情（7 天过期） | 丢进行中的任务 |
| `tasks:` | 设备活跃任务索引 | 同上 |

Redis 中无用户长期数据。`--force` 最坏情况丢失若干未读消息和进行中的任务，对于重装场景可接受。

## 不做的

- 不引入账号层
- 不做设备超时自动回收

## Acceptance criteria

- [ ] 同名设备 + 正确 password → 返回已有 API key（200）
- [ ] 同名设备 + 错误 password → 401
- [ ] 新设备名 → 正常注册（行为不变）
- [ ] 客户端 init 流程能正常处理"已有 key"的响应
- [ ] `go test ./... -count=1` 全部通过
- [ ] 服务端重新部署

## Blocked by

None
