# 10 — 代码库 Review 问题追踪

Type: HITL

> **状态注记（2026-06-20 审计）**：本 checklist 在 issue 11–22 开发期间部分项已被连带修复，但未回填勾选。本次审计逐项核对代码后更新状态。未勾选项仍为待办，但多数为"已知问题、当前无并发/扩展压力"，可按需处理。

## 问题列表

### P0 — 正确性

- [ ] **JSON marshal 错误被吞 (handlers.go)** — `msgJSON, _ := json.Marshal(...)` 等模式仍存在约 9 处。当前数据类型简单不会失败，但改类型时 bug 会被静默吞。待办：加 `if err != nil { writeError(w, 500, "internal error"); return }`。
- [x] **handleTaskResume FromDevice 中间状态错误** — 已修复。当前 `handleTaskResume` 直接用 `taskData["assigned_to"]` + `strings.SplitN`，无"先赋值 device:session 再 parse 覆盖"的中间状态。
- [ ] **PATCH sessions 非原子性 (session.go + handlers.go)** — CLI 端仍是 GET-then-PATCH 覆盖。当前单设备无并发场景，暂不处理。

### P1 — 可维护性

- [x] **HTTP 客户端样板重复** — 已修复。`pkg/cli/client.go` 抽取了 `apiDo(cfg, creds, method, url, body)` helper，所有 CLI 函数复用。
- [ ] **Redis client wrapper 无价值 (pkg/redis/client.go)** — 仍只是 embed `*redis.Client` + Ping。暂保留，若有第二类存储需求再重构。
- [ ] **TOML 解析器过于简陋 (config.go)** — 行级字符串匹配。issue 23a 会扩展支持 `[sessions]` 段的 map 解析，届时一并加固。

### P2 — 测试

- [ ] **API 测试强依赖本地 Redis** — `TestMain` 仍要求 `localhost:6379`。issue a924a27 已加 skip 逻辑（Redis 不可用时跳过），但未引入 `miniredis` 做纯单测。
- [ ] **checkPrereqs 测试依赖真实环境** — 仍依赖 `exec.LookPath`。同上，已加 skip 逻辑。
- [ ] **TestPoller_skipsWhenBusy 耗时 1s** — `waitForIdle` 的 `time.Sleep` 未响应 ctx 取消。待办。
- [ ] **覆盖率缺口** — `cmd/agentlink/main.go` 仍无测试。CLI dispatch 层，优先级低。
- [ ] **CLAUDE.md 测试用真实文件** — RunSessionAdd 已用 `t.TempDir()`。RunAttach 仍无法测 tmux attach 成功路径。

### P3 — 小问题

- [ ] **`nil` vs `[]` 不一致** — `/agents/list` 特地转 `[]AgentInfo{}`，其他 handler 返回 `null`。待统一。
- [ ] **URL 拼接无规范** — `apiDo` 内部已统一，但 `init.go` 的 `registerDevice` 仍用 `server + "/agents/register"`。待办。
- [ ] **`handlePull` limit 逻辑可读性差** — 仍为先设 1 再覆盖再 cap。待重构。

---

Blocked by: None (独立于功能开发，可并行处理)
