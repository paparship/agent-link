# 19 — Redis key 添加 `agentlink:` 命名空间前缀

Type: AFK
Blocked by: #18

## 背景

当前 Redis key 没有统一前缀：

```
device:supermicro
api_key:<hash>
inbox:supermicro:main
task:a3f8c9e1
tasks:supermicro:worker
```

这些 key 模式过于通用（`task:*`、`device:*`），如果 Redis 实例被其他应用共用，可能 key 冲突。

## 改动方案

所有 key 加 `agentlink:` 前缀：

```
agentlink:device:supermicro
agentlink:api_key:<hash>
agentlink:inbox:supermicro:main
agentlink:task:a3f8c9e1
agentlink:tasks:supermicro:worker
```

## 涉及文件

| 文件 | 改动 |
|------|------|
| `pkg/api/handlers.go` | 所有 Redis key 构造处加 `agentlink:` 前缀 |

每处构造 key 的代码都需要改。预估 15-20 处。

## 不做的

- 不做数据迁移（旧 key 不会被清理，换前缀后旧数据自然失效）
- 不提取 key 前缀为常量（当前 Go 文件中拼 key 的方式是硬编码字符串，统一加前缀即可）

## Acceptance criteria

- [ ] 所有 Redis key 以 `agentlink:` 开头
- [ ] 注册后 key 为 `agentlink:device:<name>` 而非 `device:<name>`
- [ ] 发消息后 inbox key 为 `agentlink:inbox:...` 而非 `inbox:...`
- [ ] 任务相关 key 同理
- [ ] `go test ./... -count=1` 全部通过
- [ ] 服务端重新部署后正常运作
- [ ] 旧 key（无前缀）未清理但不影响新功能

## Blocked by

- Blocked by #18（因为 #18 改了注册逻辑，同一批改 key 前缀避免冲突）
