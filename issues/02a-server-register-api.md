# 2a — 服务端注册 API + 鉴权

Type: AFK
Blocked by: 1

## What to build

服务端注册和鉴权的完整实现，不涉及 CLI。

### POST /agents/register

接收设备注册请求：

- 请求体：`{ "device": "jiefan-pc", "sessions": ["main", "worker"], "register_password": "xxx" }`
- 比对 `register_password` 与环境变量 `REGISTER_PASSWORD`，不匹配返回 401
- 校验 `device` 命名规则：仅支持小写字母、数字、连字符、下划线，长度 2-32 位，以字母开头。不合规返回 400
- 校验 `sessions` 非空，且每个 session 名符合上述命名规则
- `device` 已存在则返回 409 Conflict
- 生成 `sk_live_` + 32 位随机字符 的 API Key（crypto/rand）
- API Key 写入 Redis 时存 SHA256 哈希，明文返回给客户端
- Redis 写入 `device:<name>`（Hash 结构：sessions（JSON 数组）、api_key_hash、registered_at、last_seen）

### 鉴权中间件

在现有 `AuthMiddleware` 中实现真实鉴权逻辑：

- 放行 `GET /health` 和 `POST /agents/register`
- 其他路由从 `Authorization: Bearer <api_key>` 提取 Key
- 对 Key 做 SHA256，查 Redis 是否存在匹配的 `device:*` 记录
- 合法则设 `r.Context()` 中注入 device name（供后续 handler 使用），放行
- 无 token、格式错误、或无效 token 返回 401 `{ "error": "unauthorized" }`

### 配置传递

- `api.New()` 增加 `registerPassword string` 参数
- Handler 通过 server 字段访问 RegisterPassword
- `cmd/server/main.go` 创建 server 时传入 `cfg.RegisterPassword`

## Acceptance criteria

- [ ] `curl -X POST /agents/register` 正确密码返回 200 + API Key（JSON 中 `api_key` 字段）
- [ ] `curl -X POST /agents/register` 错误密码返回 401 `{ "error": "invalid register password" }`
- [ ] `curl -X POST /agents/register` 无效 device 名（含大写字母/超长/特殊字符）返回 400
- [ ] `curl -X POST /agents/register` 空 sessions 返回 400
- [ ] `curl -X POST /agents/register` 重复 device 返回 409
- [ ] Redis 中有 `device:<name>` Hash，包含 api_key_hash、sessions、registered_at、last_seen
- [ ] 返回的 API Key 格式为 `sk_live_` + 32 位随机字符
- [ ] Redis 中存储的是 SHA256 哈希而非明文
- [ ] `curl /health` 不带 token 正常返回（放行）
- [ ] `curl /agents/register` 不带 token 正常返回（放行）
- [ ] `curl /messages/send` 不带 token 返回 401
- [ ] `curl /messages/send -H "Authorization: Bearer sk_live_xxx"` 合法 token 放行（不校验路由是否存在）
- [ ] `curl /messages/send -H "Authorization: Bearer invalid"` 返回 401
- [ ] `curl /messages/send -H "Authorization: Bearer"`（空 token）返回 401
- [ ] 注册后返回的 API Key 能被后续鉴权通过

## Blocked by

- Blocked by #1
