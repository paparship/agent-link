# 3 — 消息收发（send + pull）

Type: AFK
Blocked by: 2

## Sub-issues

本 issue 拆分为两个独立子 issue，建议按顺序执行：

- [3a — 消息 API 服务端（send + pull）](03a-messages-api.md)
- [3b — CLI send / pull](03b-cli-send-pull.md)

## What to build

消息的端到端发送和拉取：

服务端：
- `POST /messages/send`：写入 Redis List `inbox:<device>:<session>`，返回 msg_id
- `GET /inbox/pull?limit=1`：从当前设备 session 的 inbox 中 RPOP 消息
- limit 默认为 1，最大 100
- content 上限 3000 字符，超出返回 400
- 统一鉴权（Bearer API Key）

CLI：
- `agentlink send <target> <content>` — 目标支持短名（自动补全 device）和完整 `device:session` 格式
- `agentlink pull` — 拉取 1 条
- `agentlink pull --all` — 拉取最多 10 条

## Acceptance criteria

- [ ] 同一设备两个 session 之间发消息并拉取成功
- [ ] 跨设备发消息（需注册两个设备）并拉取成功
- [ ] 短名自动补全正确
- [ ] content 超过 3000 字符被拒绝
- [ ] 消息 pull 后从队列移除
- [ ] `inbox:<device>:<session>` List 结构正确

## Blocked by

- Blocked by #2
