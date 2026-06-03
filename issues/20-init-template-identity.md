# 20 — InitTemplate 生成的 CLAUDE.md 缺少设备身份信息

Type: Code

## 背景

`agentlink init` 为每个 session 生成 `CLAUDE.md`，内容由 `adapter.InitTemplate(session)` 生成。当前模板只传入了 session name，没有传入 device name，导致 Claude Code 启动后不知道自己是谁：

- 不知道自己的 device name（如 `jiefan-local`）
- 不知道自己在哪个 session（虽然文件名写的是 main/worker，但没有显式声明）
- 不知道目标设备是谁

## 影响

Claude Code 在 tmux session 中启动后无法正确使用 agentlink 命令：

```
# 当前 CLAUDE.md 内容
- `agentlink task send <target> <task_id> "<content>"` — 发放任务
```

用户/LLM 看到 `<target>` 不知道该填什么。实际上应该写成：

```
# 期望
- 你的设备: jiefan-local
- 当前 session: main
- 对方设备: supermicro
- 发消息: `agentlink send supermicro:main "<content>"`
```

## 改动

### 接口变更

`pkg/adapter/adapter.go:22` — `InitTemplate` 增加 `device` 参数：

```go
InitTemplate(session string, device string) string
```

### 模板内容

`pkg/adapter/claude.go:30` — 模板内容加入设备身份信息：

main session 模板：
- 声明自己的 device name 和 session name
- 命令示例中的 target 用 `<device>:<session>` 格式

worker session 模板同上。

### 调用方

`pkg/cli/init.go` — 调用 `InitTemplate` 时传入 device name。

## Acceptance criteria

- [ ] `agentlink init` 后 CLAUDE.md 包含 device name 和 session name
- [ ] 命令示例中使用可读的设备身份标识
- [ ] 两端重新 init 后 CLAUDE.md 内容正确
