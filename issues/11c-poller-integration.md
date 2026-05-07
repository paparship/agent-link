# 11c — Poller 集成 Adapter

Type: AFK
Blocked by: 11a

## What to build

把 `IdleDetector` 注入 Poller，替代直接调 `IsClaudeIdle`。

**Poller struct 改动：**
```go
type Poller struct {
    // ... 现有字段
    IdleDetector adapter.IdleDetector  // 新增
}
```

**waitForIdle 改动：**
```go
// 之前: IsClaudeIdle(pane)
// 之后: !p.IdleDetector.IsBusy(pane) && p.IdleDetector.IsPromptEmpty(pane)
```

**RunPoll 改动：**
- 从 config 读 `agent` 字段
- `NewDetector(cfg.Agent)` 实例化 detector
- 注入 Poller

**IsClaudeIdle 删除：**
- `poller.go` 中的 `IsClaudeIdle` 函数删除
- 函数体已搬到 `ClaudeCodeDetector`

**Poller 测试改动：**
- `TestPoller_*` 测试中需要 mock `IdleDetector` 而非依赖 `IsClaudeIdle`
- 接口化后 mock 更干净（传 `isBusy=false` / `isPromptEmpty=true` 的组合）

## Acceptance criteria

- [ ] `Poller` 通过字段注入 `IdleDetector`，不直接调 `IsClaudeIdle`
- [ ] `RunPoll` 从 config 读 agent 类型，实例化对应 detector
- [ ] `IsClaudeIdle` 函数从 poller.go 删除
- [ ] `TestPoller_injectsWhenIdle` 通过（mock detector 返回空闲）
- [ ] `TestPoller_skipsWhenBusy` 通过（mock detector 返回忙）
- [ ] `TestPoller_skipsWhenCapturerFails` 通过
- [ ] `TestPoller_skipsWhenInboxEmpty` 通过
- [ ] `TestIsClaudeIdle` 改名为并移到 `pkg/adapter/claude_test.go`

## Blocked by

- Blocked by #11a
