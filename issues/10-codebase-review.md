# 10 — 代码库 Review 问题追踪

Type: HITL

## 问题列表

### P0 — 正确性

- [ ] **JSON marshal 错误被吞 (handlers.go:110, 250, 523 等)** — 多处 `json.Marshal`/`json.Unmarshal` 的 error 被 `_` 丢弃。虽然当前不会失败，但未来改数据类型时引入的 bug 会被静默吞掉。至少加 `if err != nil { writeError(w, 500, "internal error"); return }`。
- [ ] **handleTaskResume FromDevice 中间状态错误 (handlers.go:816)** — 先赋值为 `"device:session"` 格式，后一行才 parse 覆盖，代码混淆。直接统一用 parsed 值。
- [ ] **PATCH sessions 非原子性 (session.go + handlers.go)** — CLI 端先 GET list 再 PATCH 覆盖，两个实例同时操作丢更新。Server 端也直接 HSet 覆盖。当前无并发场景，但不稳健。

### P1 — 可维护性

- [ ] **HTTP 客户端样板重复 15+ 次** — 每个 CLI 函数独立重复 `loadConfig → loadCredentials → set auth → Do → check status → parse error body`。抽一个 `doRequest(method, url, body)` 或 client struct 封装。
- [ ] **Redis client wrapper 无价值 (pkg/redis/client.go)** — 只包了一层 `*redis.Client` 加 Ping 检查，所有消费者都直接操作 embedded client。可以去掉或者加有用抽象。
- [ ] **TOML 解析器过于简陋 (config.go:81-92)** — 行级字符串匹配，不支持转义/section/注释。当前够用但扩展时易出问题。

### P2 — 测试

- [ ] **API 测试强依赖本地 Redis (handlers_test.go)** — `TestMain` 要求 `localhost:6379` 在线。建议引入 `miniredis` 做纯单元测试，保留现有作为集成测试。
- [ ] **checkPrereqs 测试依赖真实环境 (init_test.go)** — 实际执行 `exec.LookPath("tmux")` + `exec.LookPath("claude")`。
- [ ] **TestPoller_skipsWhenBusy 耗时 1s** — `waitForIdle` 的 `time.Sleep` 不应答 context 取消。改为 select + ctx.Done()。
- [ ] **覆盖率缺口** — `cmd/agentlink/main.go` 无测试（虽然只是 CLI dispatch，但 cmdPoll 等新加命令可测）。
- [ ] **CLAUDE.md 测试用真实文件 (session_test.go)** — RunSessionAdd 测试依赖实际写文件系统，用 `t.TempDir()` 没问题，但 RunAttach 测试无法测试 tmux attach 成功路径（需要真实 tmux）。

### P3 — 小问题

- [ ] **`nil` vs `[]` 不一致** — `/agents/list` 特地把 `nil` 转 `[]AgentInfo{}`，其他 handler 直接返回 `null`。
- [ ] **URL 拼接无规范** — 部分用 `cfg.Server + "/path"`（Server 尾随 `/` 则出 `/agents//list`）。
- [ ] **`handlePull` limit 逻辑可读性差** — 先设 `limit = 1`，再条件覆盖，再 cap。可直接 `parseInt(default=1, min=0, max=100)`。

---

Blocked by: None (独立于功能开发，可并行处理)
