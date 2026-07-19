# 39 — api 测试污染生产 redis(测试必须隔离到独立 DB)

Type: BUG

## 现象

在"server + client + 开发同机、共用一个 `localhost:6379`"的部署下,每次跑 `go test ./pkg/api/`(或 `go test ./...`)都会**清空线上所有设备/session 注册**。本次调试过程中多次出现"设备注册莫名消失、心跳 401",全部由此导致——**不是重新部署 server、也不是 redis 持久化问题**。

## 根因

`pkg/api/handlers_test.go`:
```go
func TestMain(m *testing.M) {
    rdb, err := redis.NewClient("localhost:6379")   // 写死连真 redis
    ...
    cleanupTestData()      // 测试前
    code := m.Run()
    cleanupTestData()      // 测试后
}
func cleanupTestData() {
    for _, pattern := range []string{
        "agentlink:device:*","agentlink:api_key:*","agentlink:inbox:*",
        "agentlink:task:*","agentlink:tasks:*"} {
        keys, _ := testRdb.Keys(ctx, pattern).Result()
        testRdb.Del(ctx, keys...)      // 全删
    }
}
```

- 测试写死连 `localhost:6379`、用**真实 `agentlink:` 前缀**、在测试前后**把所有匹配键 DELETE**。
- 这台机器上**测试 redis 与线上 server 的 redis 是同一个实例(同 DB 0)** → 跑测试即清生产数据。
- **持久化无法防护**:测试是"删键",redis 会忠实地把"空库"持久化下来(AOF/RDB 记录的是删除后的空状态),所以开 AOF/RDB 也救不回。

## 影响

- 任何在"生产 redis 所在机器"跑测试的人(尤其本项目主打的同机部署)都会清空线上注册。
- CI 若与任何有数据的 redis 共用实例,同样受害。

## 方案:把测试彻底隔离到独立 redis DB

1. **`pkg/redis/client.go`**:`NewClient` 增加可选 DB 参数(或加 `NewClientDB(addr, db)`),默认仍连 DB 0(生产不变)。
2. **`pkg/api/handlers_test.go`(及其他连真 redis 的测试)**:`TestMain` 连**独立测试 DB**(默认 `15`,可用 `TEST_REDIS_DB` / `TEST_REDIS_ADDR` 覆盖);`cleanupTestData` 只作用于该测试 DB,**永不触碰 DB 0**。
3. **安全阀**:测试启动时,若目标 DB 里探测到疑似真实数据(存在 `agentlink:device:*`),**拒绝运行并提示**(避免有人把 `TEST_REDIS_DB` 误指到生产 DB)。

## 变更点

| 文件 | 改动 |
|------|------|
| `pkg/redis/client.go` | `NewClient` 支持 DB(默认 0) |
| `pkg/api/handlers_test.go` | `TestMain` 连测试 DB(默认 15);cleanup 仅该 DB;加安全阀 |
| 其他连 `localhost:6379` 的测试(若有) | 同样切到测试 DB |

## 不做

- 不改生产 key 前缀/schema。
- 不引入独立测试 redis 进程的编排(用同实例的独立 DB 即可)。

## 验收

- `go test ./pkg/api/` 运行后,DB 0 里的 `agentlink:*` 生产数据**不受影响**(可先写入一条,跑测试,确认仍在)。
- 目标测试 DB 若含真实数据 → 测试拒绝运行并给出明确提示。
- 全量测试仍通过。
