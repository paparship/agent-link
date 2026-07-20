# 42 — uninstall / 重装后 server 侧残留:processing 泄漏、tmux 清理依赖磁盘目录、register 复用不重置 sessions

Type: BUG

## 现象(实测现场)

`agentlink uninstall` 后重新安装 + `init`,发现:

- `agentlink list --all` 仍列出上一局的 6 个 session(`main worker undercover-dm bob alice mike`),而本次 init 只建了 `main`/`worker`;
- `agentlink session add undercover-dm` 报 `session "undercover-dm" already registered on device`;
- redis 里 `agentlink:processing:jiefanlin:undercover-dm` 一直清不掉。

现场跑一次 uninstall,before/after 对比确认:device / api_key / inbox / base_dir / ~/.agentlink / binary / tmux 都清干净了,**唯独 `processing:jiefanlin:undercover-dm` 漏下**。

## 根因(三处)

### 1. `deleteDeviceData` 从不删 `processing:*`(pkg/api/handlers.go)
issue 37 引入了 `agentlink:processing:<device>:<session>`(reserve 后暂存),但 `deleteDeviceData` 只删了 `inbox:*` / `tasks:*` / `task:*` / `device` / `api_key`,漏了 `processing:*`。→ 每次注销都泄漏 processing 列表(内含 reserve 未 ack 的消息)。

### 2. `killSessionSessions` 靠磁盘目录枚举要杀的 tmux(pkg/cli/runtime/session.go)
它 `os.ReadDir(baseDir)`,对每个子目录名 `kill-session`。若 `base_dir` 已不存在(半清理状态、或目录先被删),`ReadDir` 直接返回 → **一个 tmux 都不杀** → tmux 泄漏。tmux 要杀哪些的真相源应是注册的 session 列表 / config 的 `[sessions]`,不该依赖磁盘目录是否还在。

### 3. register 复用分支不重置 `sessions`(pkg/api/handlers.go, handleRegister)
device 已存在时走「幂等复用」:轮换 api_key、刷新 `registered_at`/`last_seen`,但**不动 `sessions` 字段**。于是当 device 侥幸存活(uninstall 的 best-effort 注销失败、或本地被手动清而 server 没清),重装 init 会复用旧 device 并保留陈旧的 `sessions` 数组 → ghost session + "already registered"。

外加陈旧注释:该分支注释仍写 `agentlink uninstall --purge`,而 `--purge` 已在 issue 40 移除。

## 方案

- **`deleteDeviceData`**:补清所有 per-device:session 遗留 key —— `processing:*`、`current_msg:*`、`issued:*`(排查发现这三个前缀都漏了,不止 processing;与 inbox 同样 `Keys`+`Del`)。测试 `cleanupTestData` 也补上这三个前缀。
- **`killSessionSessions`**:改用 config 的 `[sessions]`(`cfg.Sessions` 的 key 集合)来决定杀哪些 tmux,不再依赖 `os.ReadDir(baseDir)`。base_dir 缺失时仍能正确清 tmux。
- **register 复用分支**:把 `sessions` 一并 `HSet` 重置为本次 `req.Sessions`(自愈)。init 有 "目录已存在则先 uninstall" 的守卫,复用分支本就只在「device 残留」的恢复场景命中,重置是正确语义。
- 修掉 `--purge` 陈旧注释。

## 验证

- `go build ./...`、`go test ./pkg/api/... ./pkg/cli/...` 通过。
- 现场:uninstall 后 `redis KEYS 'agentlink:*'` 为空(含 processing);base_dir 先删再 uninstall 也能杀净 tmux;残留 device 上重装 init 后 `list --all` 只剩本次 session。
