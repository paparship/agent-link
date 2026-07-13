# 34 — session id 生命周期修复(合并关闭 #29 #30)

Type: BUG

## 背景

合并 #29(restart 丢 id / 不写回 config)与 #30(session add 不写 config)。二者是**同一个病根**:整套逻辑依赖"启动 claude 后从 `~/.claude.json` 回读 `lastSessionId`",而这条路线经源码核对后被确认**从根上不可行**。故关闭 #29 #30,由本 issue 统一给出完整方案。

## 源码核对结论(Claude Code v2.1.197)

对 `claude` 二进制(Bun 单文件打包)反查后确认以下机制:

### session id 如何管理

- 内存里由全局状态 `Bt.sessionId` 持有,取值器 `Lt() = g0()?.sessionId ?? Bt.sessionId`。
- 初始值:`Bt.sessionId = nIt() ?? randomUUID()`。`nIt()` 只读环境变量 `CLAUDE_CODE_REMOTE_SESSION_ID`(remote 场景)。所以**普通启动 = 每次随机**,除非用 `--session-id` / `--resume` 覆盖。
- 全局**只有两个函数**会重新赋值 `Bt.sessionId`:
  - `tSr()`:置为**新 `randomUUID()`**,发 `"clear"` 事件。全局**唯一调用者 = `/clear`**(conversation_reset)。
  - `BA(e,…)`:置为**给定 id**,用于 `--resume` / `/resume` / fork。
- 每个 session id ↔ 一个 transcript 文件 `~/.claude/projects/<编码后的 cwd>/<sessionId>.jsonl`(由 `Zf()` 拼接)。**换 id = 换文件**。

### 什么行为会改变一个窗口的 session id

| 行为 | 是否改变 id | 说明 |
|------|:---:|------|
| `/clear` | **是** | `tSr()` 生成全新 UUID,并开新 `.jsonl`;旧 id 被抛弃 |
| `--resume` / `/resume` / fork | 是 | `BA()` 切到另一个已存在的 id |
| 裸启动(无 `--session-id`) | — | 每次随机新 id |
| `/compact` | **否** | 原地摘要,不走 `tSr`;id 与 transcript 不变 |

### `lastSessionId` 的落盘时机(关键)

`projects[<cwd>].lastSessionId` 由 `y6t()`→`mH()`(per-project 合并)写入。其**全部调用点只在两类时机**:

1. `process.on("exit")` —— claude **退出时**;
2. resume/fork 切换会话前 flush。

**会话运行期间根本不写。** 启动时只写 `lastGracefulShutdown:false`,不含 sessionId。顶层 `lastSessionId` 实际从不被填充。

### 可用的显式控制

- `--session-id <uuid>`:"Use a specific session ID for the conversation (must be a valid UUID)"。可在启动时**指定**一个已知 id。
  - 约束(源码报错文案):`--session-id` 与 `--resume/--continue` 同用**必须再加 `--fork-session`**。
- `--continue`:恢复该 cwd 下**最近一次**会话(按时间)。

## 根因(修正版)

- **#29A**(次要):`resume.go` 调 `launchSessions` 后用 `_` 丢弃返回的 id map,录到的 id 不写回 config。
- **#29B**(真正病根,原分析不准):`readClaudeSessionID` 读**顶层** `lastSessionId`。实际该字段 (a) 在 `projects[<cwd>]` 下、(b) **仅在退出/切换时写**。故启动后等 10s 回读 → 拿到上一次会话的旧 id 或空。
- **#30**:`session.go` 的 `&& sid != ""` 使 id 为空时整个跳过写 config;因 #29B 恒空,新 session 名永不入 config。

## 方案

**核心思路转变:agentlink 不再向 Claude "回读" id,而是自己"掌握" id。**

### 1. 新建会话 —— 自生成 id,`--session-id` 传入

`init` / `session add` 时,agentlink 侧生成 UUID,启动 `claude --session-id <uuid> --dangerously-skip-permissions`。id 是自己造的、**立即写入 config,无需任何回读**。

→ 一举消除 #29A(无 map 可丢)、#29B(不再回读)、#30(写 config 时 id 恒非空)。

### 2. restart —— 用 `--continue`,而非 `--resume <stored-id>`

每个 agentlink session 独占自己的工作目录(`main/`、`worker/`…),即**每个 cwd 只有单一会话谱系**。因此 `--continue`(取该目录最近会话)既精确、又天然**扛住 `/clear`**:

- 若用 `--resume <stored-id>`:agent 在窗口里 `/clear` 过之后,stored-id 指向的是 **/clear 之前**的旧对话,恢复错误。
- 用 `--continue`:恢复的是最近的 `.jsonl`,即 /clear **之后**的当前对话,符合用户所见。

> config 里存的 id 在 /clear 后会"过时",但它只用于展示/审计,不影响 restart 正确性。(可选增强:restart 时扫 `~/.claude/projects/<cwd>/` 里 mtime 最新的 `.jsonl` 刷新 config 的 id;非必需。)

### 3. adapter 接口调整

`AgentLauncher` 增加两种启动参数构造(命名待定):

- 新建带指定 id:`NewSessionArgs(sessionID string) []string` → `--session-id <id> --dangerously-skip-permissions`
- 恢复:复用/改造 `ResumeArgs`,restart 场景返回 `--continue --dangerously-skip-permissions`

`TclaudeLauncher` 继承同一实现(tclaude 原样转发参数,`~/.tclaude/` 结构一致)。

> **与 #33 的关系**:#33 为回读逻辑引入了 `SessionIDPath()`。本方案取消回读,`SessionIDPath()` 随之**不再需要,应移除**。若 #33 尚未合入 master,可在合入时直接省去该抽象。

## 变更点

- `pkg/adapter/adapter.go` / `claude.go` / `tclaude.go`:新增 `NewSessionArgs(id)`;移除 `SessionIDPath()`(#33 引入)。
- `pkg/cli/runtime/init.go`:
  - 生成 UUID,`launchSessions` 走 `--session-id`;
  - 删除 `readClaudeSessionID` / `readClaudeSessionIDWithTimeout` 回读逻辑;
  - id 直接来自自生成值,写入 config。
- `pkg/cli/runtime/session.go`(`RunSessionAdd`):去掉 `&& sid != ""`,session 名 + 自生成 id 无条件写 config。
- `pkg/cli/runtime/resume.go`:改用 `--continue`;(返回值已无需接住)。
- `pkg/cli/net`:`UpdateSessionID` 拆分为"确保 session 存在于 config"+"更新 id"两步,或提供 `EnsureSession(name, id)`。

## 验收

- `init` 完成后,`~/.agentlink/config.toml` 的 `[sessions]` **立即**有非空 id(不再是 `main = ""`)。
- `agentlink session add reviewer` 后,config `[sessions]` 含 `reviewer`。
- `agentlink restart` 后,`attach main` / `attach worker` 恢复到各自**正确的对话**,不再显示 `(session_id unavailable, continue fallback)`。
- 在窗口内执行 `/clear` 后再 `restart`:恢复的是 /clear **之后**的对话,而非旧对话。
- `--agent claude` 与 `--agent tclaude` 均通过。
