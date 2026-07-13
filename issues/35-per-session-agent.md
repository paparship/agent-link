# 35 — per-session agent 类型(每个 session 自选 CLI)

Type: AFK

## 背景

agent 类型(claude / tclaude)当前是**设备级**:存在 `config.toml` 顶层一行 `agent = "..."`,`LoadConfig` 读进 `cfg.Agent`,全设备所有 session 共用。这偏离了最初设计初衷——**每个 agent session 在创建时应能各自指定用哪个 CLI**。

adapter 的可插拔机制(`AgentLauncher` / `IdleDetector` + 按名构造)其实已经就位,运行时天然支持多种 agent 并存;缺的只是把"agent 类型"这个值从设备作用域下沉到 session 作用域,并设计好创建时的选择流程。

本 issue 依赖 #34 已落地(launchSessions 已改为自生成 `--session-id` + restart `--continue`),在其之上把 agent 维度也下沉到 per-session。**不涉及 server / Redis 改动。**

## 设计原则(重要)

1. **不可变**:session 的 agent 类型在**创建时写一次,之后永久不可更改**,不提供任何修改命令。
2. **不改 CLAUDE.md**:唯一注入的指引仍只是"run `agentlink whoami` first"。所有引导都表现为 **agent 运行命令后看到的 CLI 输出**。
3. **不区分人 / agent 输入**:除 install / uninstall / attach / init / restart 这几个人用的命令外,其余命令一律由 agent 运行。所以 `session add` 只有一条路径——agent 运行、读输出——**没有交互式 TTY prompt**。
4. **单一数据源**:"支持哪些 agent""本机装了哪些"永远只从 CLI(adapter registry + PATH 探测)得出,不硬编码进任何提示文案。
5. **容忍偶尔自作主张**:agent 有可能没确认就选了个类型;不靠事前强拦死,而靠**创建后的回显**把"这次建成了什么"摊给用户,可被及时发现、remove 重建。

## 方案

### A. adapter registry(替换硬编码 switch,单一数据源)

现在 `NewLauncher` / `NewDetector` 是两个写死的 `switch`,没有可读的"支持列表"。改为注册表:

```go
var registry = map[string]agentSpec{
    "claude":  {...},
    "tclaude": {...},
}
func SupportedAgents() []string          // registry 的 key,排序返回
func AvailableAgents() []string          // SupportedAgents 中二进制在 PATH 上的子集
```

`NewLauncher` / `NewDetector` 改为查 registry。**加一个新 CLI = 注册一行**,whoami 提示、缺类型指引、`--type` 校验全部自动跟上,无需回改文案。

### B. 存储:per-session `.agentlink.toml`,写一次

- `.agentlink.toml` 增加 `agent = "<type>"`;`WriteSessionTOML` 加 agent 参数。创建时写入,之后不重写。
- 新增 `LoadSessionConfig(dir)`(或扩展 `FindCurrentSession` 一并返回 agent)。
- `config.toml` 顶层 `agent =` **保留为设备级默认/回退**:老 session 的 `.agentlink.toml` 无该字段时回退到它(再回退 claude),保证平滑兼容。

### C. `session add`(agent 运行,纯输出驱动的状态机)

永远 agent 在跑,**无 TTY 交互**;行为完全由输出决定:

| agent 执行 | CLI 输出 |
|---|---|
| `session add --type <t> <name>`,`<t>` 合法且已装 | **创建 + 回显**(见下) |
| `--type <t>` 合法但未装 | 拒绝:`<t> not found on this machine; install it (or "<t> login") first` |
| `--type <t>` 未知 | 拒绝:`unknown agent type "<t>"; supported: <SupportedAgents()>` |
| 未带 `--type` | 输出 **NEEDS_AGENT_TYPE 指引块**,不创建 |

**NEEDS_AGENT_TYPE 指引块**(内容自足,列表由 registry + 探测生成):

```
NEEDS_AGENT_TYPE: session "<name>" needs an agent type (permanent, cannot be changed later).
Supported on this machine:
  - claude    (installed)
  - tclaude   (installed)
Next:
  - if the user already chose a CLI, re-run: agentlink session add --type <claude|tclaude> <name>
  - otherwise ask the user which one, then re-run with --type
```

**创建成功回显**(把关落在这里):

```
✓ session "<name>" created — agent: <type> (permanent)
  Report this agent type to the user to confirm.
```

即使 agent 未经确认就猜了类型,这行回显也会把结果暴露给用户;要改只能 `session remove <name>` 后重建。

### D. whoami 命令清单(agent 得知 add 语法的唯一来源)

`whoami` 的 "Commands (for agent use):" 里那行改为(`<...>` 由 `SupportedAgents()` 动态拼,非硬编码):

```
  agentlink session add --type <claude|tclaude> <name>   — create new session (confirm agent type first, permanent)
```

- `--type ...` 摆进语法 → 用户已明说类型时,agent 直接一步选对。
- `confirm agent type first, permanent` → 提示先确认、且不可改。
- 未从 whoami 读到 / 用户没说清 → 落到 C 的指引块。

### E. init(人用命令,例外)

`init` 属于人跑的基础命令,可交互:创建 main / worker 时**各问一次**类型(列出 registry + 探测标注,回车 = 第一个默认值),不预设统一类型。**去掉 `init` 上原有的 `--agent` flag**(它语义是"给整设备统一赋一个类型",与 per-session 冲突)。

### F. 类型下沉的线程改造(把 `cfg.Agent` 换成 per-session)

逐 session 读取该 session 的 agent,替换现有对设备级 `cfg.Agent` 的使用:

- `launchSessions`:循环里对每个 session 读其 `.agentlink.toml` 的 agent(toml 在 launch 前已写),分别 `NewLauncher`。
- `attach`(session.go):读该 session 的 agent。
- `poller`(poller.go):`NewDetector(<本 session 的 agent>)` —— poller 已通过 `FindCurrentSession` 知道自己在哪个 session。
- `restart`(resume.go):逐 session 读 agent。

### G. 可见性

`whoami` / `list` 为每个 session 显示其 agent 类型,创建后随时可查、可核对。

## 不做 / 边界

- 不改 server、不改 Redis、不改 CLAUDE.md 注入内容(除 whoami 输出外)。
- 不提供修改已存在 session 的 agent 类型的命令(不可变)。
- 探测只用于"标注 / 排 error / 回显",不用于从清单里删项(避免 wrapper/alias 被 `LookPath` 漏判而堵死用户)。

## 验收

- `.agentlink.toml` 含 `agent`;创建后该字段不被任何命令改写。
- `session add --type tclaude coder` → 创建,回显 `agent: tclaude (permanent)`,`whoami`/`list` 显示 coder 为 tclaude。
- `session add coder`(无类型)→ 输出 NEEDS_AGENT_TYPE,**不创建**。
- `session add --type bogus coder` → 报未知类型,列出 `SupportedAgents()`。
- `session add --type tclaude coder`(tclaude 未装)→ 报未找到,提示安装 / 登录。
- main / worker 可为不同类型;`restart` 后各自用正确 CLI 恢复。
- 老 config(session 无 per-session agent)→ 回退设备默认,行为不变。
- whoami 的 `session add` 行的 `<...>` 随 registry 变化,新增 agent 无需改文案。
