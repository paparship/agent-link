# 2 — 设备注册（注册 API + init CLI）

Type: AFK
Blocked by: 1

## Sub-issues

本 issue 拆分为两个独立子 issue，建议按顺序执行：

- [2a — 服务端注册 API + 鉴权](02a-server-register-api.md)
- [2b — CLI init 命令](02b-cli-init.md)

## What to build

设备注册的端到端能力：服务端注册 API + CLI `init` 命令。

服务端：
- `POST /agents/register`：接收 device、sessions、register_password
- register_password 比对环境变量 `REGISTER_PASSWORD`，错误返回 401
- 写入 Redis：`device:<name>`（含 sessions、api_key_hash、registered_at）
- 生成 `sk_live_` + 32 位随机字符 的 API Key
- API Key SHA256 哈希存储，明文返回给客户端
- 所有后续请求通过 `Authorization: Bearer <api_key>` 鉴权

CLI：
- `agentlink init --server <url> --password <pw> [--device <name>] [./path]`
- `--device` 可选，省略时自动取 hostname
- 前置检查：tmux 和 claude 命令是否存在
- 创建目录（默认 `./agent_team/` + `main/` + `worker/`）
- 写入 `~/.agentlink/config.toml`（server、device、base_dir）
- 调用注册 API，写入 `~/.agentlink/credentials.json`
- 写入 `main/.agentlink.toml` 和 `worker/.agentlink.toml`

服务端鉴权：
- 鉴权中间件查 Redis 校验 Bearer token（SHA256 比对）
- 放行 `GET /health` 和 `POST /agents/register`
- 其他路由需要合法 API Key，否则 401

## Acceptance criteria

- [ ] `POST /agents/register` 正确密码返回 API Key，错误密码返回 401
- [ ] `agentlink init --server http://localhost:8080 --password xxx` 成功执行
- [ ] `agentlink init --server http://localhost:8080 --password xxx --device my-pc` 使用自定义设备名
- [ ] `~/.agentlink/config.toml`、`credentials.json` 正确生成
- [ ] `agent_team/main/.agentlink.toml` 和 `agent_team/worker/.agentlink.toml` 正确生成
- [ ] Redis 中有 `device:<hostname>` 或 `device:my-pc` 记录
- [ ] 鉴权中间件：无 token 或错误 token 返回 401，合法 token 放行
- [ ] 前置检查：tmux 不存在时报错退出

## Blocked by

- Blocked by #1
