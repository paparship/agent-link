# 22 — Poller 体验优化：注入标识、文案修正、CLAUDE.md 指引

Type: Code

## 背景

poller 上线后发现三个体验问题：

1. Agent 分不清注入消息和用户输入
2. 状态面板的"积压"语义不准
3. CLAUDE.md 指引未考虑 poller 自动注入场景

## 改动

### 1. 注入消息加标识头

当前 poller 直接把 `msg.Content` 打入 prompt，Claude Code 无法区分是用户输入还是外部队列注入。

**`pkg/cli/poller.go`** — `sendKeys` 前在 content 前加标识头：

```
[来自 YOUR_HOSTNAME:main 的消息]
你好！我是...
```

格式：`[来自 <device>:<session> 的消息]\n<content>`

仅对 type=msg 的消息加标识头，type=task 保持原样（已有 "Task from xxx" 描述）。

### 2. 状态面板"积压"修正

**`pkg/cli/messages.go`** — `displayRecipientStatus`：

- "积压: N 条待读取消息" → "未读: N 条"
- N=0 或 N=1 时不显示（1 条是刚发的那条自己，不算阻塞）

### 3. CLAUDE.md 指引补充

**`pkg/adapter/claude.go`** — main session 模板：

`agentlink pull` 的说明改为：

```
- `agentlink pull` — 拉取消息（poller 开启时自动注入，无需手动 pull）
```

## Acceptance criteria

- [ ] 注入消息显示 `[来自 <device>:<session> 的消息]` 标识头
- [ ] task 注入不受标识头影响
- [ ] 状态面板显示"未读: N 条"（N≥2 时显示）
- [ ] CLAUDE.md pull 说明标注 poller 自动注入
