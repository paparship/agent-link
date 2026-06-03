# Integration Test Design

## Goal

覆盖 agentlink 所有功能端到端，可在本地（CI）或双机环境运行。

## 测试架构

```
┌─────────────────────────────┐     ┌─────────────────────────────┐
│  Test Runner (host machine) │     │  Server (YOUR_SERVER_IP)     │
│                             │     │                             │
│  agentlink CLI (device-a)   │────▶│  agentlink-server           │
│  curl → API                 │     │  agentlink CLI (device-b)   │
│                             │     │  redis-cli                  │
└─────────────────────────────┘     └─────────────────────────────┘
```

## 测试场景

### 1. API 层测试（curl）

不依赖 CLI 配置，直接测试服务端 API：

| 场景 | 方法 | 端点 | 验证 |
|------|------|------|------|
| 健康检查 | GET | /health | 200 + redis connected |
| 注册 | POST | /agents/register | 200 + api_key |
| 重复注册（同名+同密码） | POST | /agents/register | 200（复用） |
| 重复注册（错误密码） | POST | /agents/register | 401 |
| 发消息 | POST | /messages/send | 200 + message id |
| 拉消息 | GET | /inbox/pull | 200 + items |
| 空收件箱 | GET | /inbox/pull | 200 + empty items |
| 发任务 | POST | /tasks/send | 200 + task_id |
| 任务忙检测 | POST | /tasks/send | 409 + recipient_status |
| 完成任务 | POST | /tasks/result | 200 |
| 取消任务 | POST | /tasks/cancel | 200 |
| 任务状态 | GET | /tasks/status | 200 + status |
| 活跃任务列表 | GET | /tasks/list | 200 + tasks |
| 心跳 | POST | /agents/heartbeat | 200 |
| 设备列表 | GET | /agents/list | 200 + devices |
| 消息状态查询 | GET | /messages/status | 200 + pending/delivered |
| 消息不存在 | GET | /messages/status | 404 |
| 删除设备 | DELETE | /agents/device | 200 |

### 2. CLI 层测试（双机）

依赖两台已 init 的设备，测试跨设备交互：

| 场景 | 命令 | 验证 |
|------|------|------|
| 心跳 | `agentlink ping` | 状态变 online |
| 设备列表 | `agentlink list --all` | 显示两个设备 |
| 发消息（A→B） | `agentlink send` | 状态面板显示 |
| 拉消息（B） | `agentlink pull` | 收到消息 + 显示 ID |
| 消息状态查询 | `agentlink message status` | pending → delivered |
| 发任务（A→B） | `agentlink task send` | 返回 task_id |
| 任务状态（A） | `agentlink task status` | issued → in_progress |
| 完成任务（B） | `agentlink task result` | completed |
| 取消任务 | `agentlink task cancel` | cancelled |
| 活跃任务列表 | `agentlink task list` | 只显示活跃的 |

### 3. 边界和错误场景

| 场景 | 预期 |
|------|------|
| 无效 device name | 400 |
| 空 content | 400 |
| 超长 content（>3000） | 400 |
| 不存在的 target | 404 |
| 未认证请求 | 401 |
| 不存在的 task_id | 404 |
| 已完成 task 再 cancel | 400 |

### 4. 需要双机交互的场景（手动 / 脚本轮询）

| 场景 | 流程 |
|------|------|
| 设备忙碌时 send → 状态面板 | B 先 pull task (变 busy)，A send → 显示 busy |
| device offline 时 send | B 停心跳超过 2 分钟，A send → 显示 offline |
| 中断 --interrupt | A 发 task，B busy 时 A send --interrupt → B 被中断 |
| 多消息未读 | A 连续发 3 条 → B pull → 看到全部 + 未读计数 |

## 输出格式

```
=== API: Health ===
  PASS  200 + redis connected

=== API: Register ===
  PASS  device 'test-integration' registered

=== CLI: Send ===
  PASS  message sent, status panel shows idle

=== CLI: Message status ===
  PASS  pending → delivered after pull

=== API: Error handling ===
  PASS  empty content returns 400

────────────────────────────────
  12/12 passed
────────────────────────────────
```

## 实现方式

脚本化：一个 bash + curl 脚本做 API 层测试，一个配套 SSH 脚本做双机 CLI 测试。

API 测试可独立运行（不依赖 init），CLI 测试需要先 `deploy.sh` 确保环境就绪。
