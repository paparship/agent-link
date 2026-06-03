# 21c — 中断机制 --interrupt

Type: AFK

## 背景

agent 执行长任务期间完全失明。对于需要紧急响应的情况（取消任务、紧急变更），发送者需要一种方式通知忙碌的 agent。

## 设计

### CLI

`send` 和 `task send` 新增 `--interrupt` 标记：

```bash
agentlink send --interrupt supermicro:main "立刻停止，有紧急问题"
agentlink task send --interrupt supermicro:main fix-001 "紧急修复"
```

### 服务端

收到带 `interrupt: true` 的消息时：

1. 投递到 inbox（同普通消息）
2. 标记该消息为 `interrupt: true`
3. 如果目标 session 有 in_progress task，task 状态不变（poller 侧做打断）

### Poller

poller 拉取到带 interrupt 标记的消息时：
1. 检测当前会话是否忙碌（`IsBusy`）
2. 如果忙碌，向 tmux pane 发送 Ctrl+C 或 ESC 打断当前操作
3. 然后注入消息到 prompt

### 安全性

- `--interrupt` 不做额外鉴权（有 API key 即可打断）
- 被打断的 task 状态不变，由接收方 agent 自行决定后续处理

## 待讨论

- 打断后是否需要自动 task suspend？
- 是否需要限制打断频率（如 5 分钟内只能打断一次）？

## Acceptance criteria

- [ ] CLI 支持 `send --interrupt`
- [ ] CLI 支持 `task send --interrupt`
- [ ] 服务端存储 interrupt 标记
- [ ] poller 检测 interrupt 标记并打断忙碌 agent
- [ ] 非忙碌时 interrupt 标记无额外影响
