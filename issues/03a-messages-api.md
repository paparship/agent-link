# 3a — 消息 API 服务端（send + pull）

Type: AFK
Blocked by: 2a

## What to build

消息收发的服务端 API，包含发送和拉取两个端点。

### 数据模型

收件箱使用 Redis List，LPUSH 写入，RPOP 消费：

```
Key: inbox:<device>:<session>
Type: List
Value: JSON 字符串
```

消息 JSON 格式：

```json
{
  "id": "uuid",
  "type": "msg",
  "from_device": "jiefan-pc",
  "from_session": "main",
  "content": "hello",
  "created_at": "2026-05-03T12:00:00Z"
}
```

### POST /messages/send

向目标 session 的收件箱发送一条消息。

```
Auth: Bearer <api_key>
Body: {
  "to": "device:session",
  "from_session": "main",
  "content": "hello"
}
Response 200: { "id": "uuid" }
```

**请求字段：**
- `to`（必填）：目标身份 `device:session`，格式校验同 device/session 命名规则（小写字母/数字/连字符/下划线，2-32 位，字母开头）
- `from_session`（必填）：发送方的 session 名，格式同上
- `content`（必填）：消息内容，上限 3000 字符，不允许为空

**处理流程：**
1. 鉴权通过后，从 context 获取 device 作为 `from_device`
2. 解析 `to` 为 `target_device:target_session`
3. 校验 all 字段格式
4. 校验 content 长度 ≤ 3000
5. 检查目标设备是否存在（`EXISTS device:<target_device>`），不存在返回 404
6. 检查目标 session 是否在该设备的 sessions 列表中
7. 生成 UUID，组装消息 JSON
8. `LPUSH inbox:<target_device>:<target_session>` 写入消息
9. 返回 `{ "id": "uuid" }`

**错误响应：**

| 状态码 | 场景 |
|--------|------|
| 400 | 缺少必填字段、content 超长、字段名格式错误 |
| 401 | API Key 无效 |
| 404 | 目标设备不存在、目标 session 不在设备 sessions 列表中 |

### GET /inbox/pull

从当前设备指定 session 的收件箱中消费消息。

```
Auth: Bearer <api_key>
GET /inbox/pull?session=worker&limit=1

Response 200: {
  "items": [
    {
      "id": "uuid",
      "type": "msg",
      "from_device": "jiefan-pc",
      "from_session": "main",
      "content": "hello",
      "created_at": "..."
    }
  ]
}
```

**参数：**
- `session`（必填）：当前设备的哪个 session 拉取
- `limit`（可选）：拉取数量，默认 1，最大 100。小于 1 或未传则用 1，超过 100 用 100

**处理流程：**
1. 鉴权通过后，从 context 获取 device
2. `RPOP inbox:<device>:<session>` 循环执行 limit 次
3. 返回消息列表（无消息时返回空数组）

**错误响应：**

| 状态码 | 场景 |
|--------|------|
| 400 | 缺少 session 参数 |
| 401 | API Key 无效 |

## Acceptance criteria

### POST /messages/send

- [ ] 发送合法消息返回 200 + `{ "id": "uuid" }`
- [ ] 缺少 `to` 返回 400
- [ ] 缺少 `from_session` 返回 400
- [ ] 缺少 `content` 返回 400
- [ ] `content` 为空字符串返回 400
- [ ] `content` 3001 字符返回 400
- [ ] `content` 正好 3000 字符成功（边界）
- [ ] `to` 不含冒号（如 `"worker"`）返回 400
- [ ] `to` 中 device 名无效（大写字母）返回 400
- [ ] `to` 中 session 名无效返回 400
- [ ] `from_session` 无效返回 400
- [ ] 目标设备不存在返回 404
- [ ] 目标 device 存在但 session 不在其 sessions 列表中返回 404
- [ ] 消息写入 Redis `inbox:<device>:<session>` 作为 List 的左侧（LPUSH）
- [ ] Redis 中存储的消息 JSON 包含 id、type、from_device、from_session、content、created_at
- [ ] `from_device` 由服务端从 API Key 鉴权结果填充，与请求体无关

### GET /inbox/pull

- [ ] 拉取成功返回 200 + `{ "items": [...] }`
- [ ] 收件箱为空时返回 `{ "items": [] }`
- [ ] 拉取后消息从队列移除（确认 RPOP 语义）
- [ ] 发送 3 条，`limit=1` 拉取 1 条，收件箱剩 2 条
- [ ] `limit=10` 拉取全部 3 条，收件箱清空
- [ ] 发送 5 条，`limit=3` 拉取 3 条，收件箱剩 2 条
- [ ] `limit` 缺省时拉取 1 条
- [ ] `limit=0` 拉取 1 条（小于 1 的按默认 1）
- [ ] `limit=200` 拉取最多 100 条（超过 100 的截断）
- [ ] `limit` 为负数返回 400
- [ ] 缺少 `session` 参数返回 400

### 鉴权

- [ ] `/messages/send` 无 token 返回 401
- [ ] `/messages/send` 无效 token 返回 401
- [ ] `/inbox/pull` 无 token 返回 401
- [ ] `/inbox/pull` 无效 token 返回 401

### 集成场景

- [ ] 同一设备两个 session 之间发消息并拉取成功（如 `main→worker`）
- [ ] Redis 中 inbox List 数据格式完整可解析

## Blocked by

- Blocked by #2a
