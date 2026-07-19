# 40 — CLI 人机流转、提示指引与全英文统一(含修正 #38 的 uninstall)

Type: BUG

## 背景

全链路审计发现:提示/状态流转有几处"人该自然流转到下一步却断了"的断点;`#38` 把 `uninstall` 默认语义做反了;且**输出语言中英混排**(agent 面本应全英文却有 4 处中文,人面向导又整块中文)。本 issue 一次收口。

**原则**
- `--help` 是纯参考手册,不当"上手/流转"入口;**每个命令的成功与报错输出都点名下一个自然命令**。
- **所有面向用户/agent 的 CLI 输出统一英文**(消除中英混排)。

人的生命周期:`install.sh → agentlink init(默认交互式)→ [自动进 main,之后与 agent 对话]`;`restart` / `uninstall` 为维护态。

## A. 修正 #38:`uninstall` 默认即注销 server(best-effort),去掉 `--purge`

`#38` 做成"默认只清本地、`--purge` 才注销" —— 语义反了:默认不注销会在 server 留**幽灵设备**(仍在 `list`、别人还能 send/task、inbox 堆积)。`#38` 真正要修的只是"注销失败就 return、本地清不干净"。**正解是把注销改 best-effort,而非改成 opt-in**:

```
agentlink uninstall = 注销 server(失败仅 warning) + 一定清干净本地(base_dir + ~/.agentlink + 二进制)
```
- 去掉 `--purge`;重装走 `uninstall → init`(幂等)即可。
- 覆盖 `#38` 的 uninstall 部分与 issue 18 的 "local-only + --purge" 设计;`#38` 其余(删 `init --force`、删 server force 死分支)保持不变。

## B. `install.sh` 指引到 `agentlink init`(裸命令)

默认主路径是 **`agentlink init` 直接跑走交互式注册**;带 `--server/--password` 是快速/测试的非常规路径。结尾 `Run 'agentlink init --help'` 改为:
```
✓ agentlink installed (version …)
Next: run 'agentlink init'
```

## C. `init` 收尾死提示

`RunInit` 末尾 `Next steps: agentlink attach worker`,随后 `cmdInit` 立刻自动 attach 进 main 的 TUI —— 这行**瞬间被盖掉、人看不到**,且它让你 attach worker、实际进的是 main。改为进 main **之前**一句能看到、描述真实动作的英文提示,例如:
`Entering main session (detach with Ctrl-b d). Other session: agentlink attach worker`

## D. 未初始化时的死路

任何需配置的命令未 init 时,`LoadConfig` 只吐 `config file not found at <path>`,不指引 init。追加:
`config file not found at <path>; run 'agentlink init' first`

## E. 全英文统一(消除中英混排)

把所有仍是中文的 user-facing 输出转英文。清单:

**Agent 面(agent 读/被注入,必须英文)**
| 位置 | 现 | 改 |
|------|----|----|
| `poller.go` 消息注入前缀 | `[来自 X:Y 的消息] ` | `[message from X:Y] ` |
| `poller.go` 任务注入指引 | `[来自…的任务…] 完成后请执行:… 如需挂起:…` | `[task … from X:Y] … When done: agentlink task result … completed "<result>" / To suspend: … suspended "<reason>"` |
| `messages.go` send 回执 | `✓ 消息已投递（ID: %s）` | `✓ message delivered (id: %s)` |
| `messages.go` 状态行 | `… session 当前状态: …` | `… session status: …` |

**人面(向导等)**
| 位置 | 现 → 改 |
|------|--------|
| `init_wizard.go` 向导 | `交互式初始化…`→`Interactive agentlink setup (Enter = [default])`;`server 不能为空`→`server must not be empty`;`正在探测 …/health`→`probing …/health`;`连接失败`→`connection failed`;`✓ 已连接`→`✓ connected`;`无法连接到 server(重试3次后放弃)`→`could not connect to server (gave up after 3 tries)`;`注册密码`→`register password`;`密码不能为空`→`password must not be empty`;`未输入密码…`→`no password entered (gave up after 3 tries)`;`设备名`→`device name`;`开启/关闭`→`on/off`;`即将初始化:`→`About to initialize:`;`确认继续?`→`Proceed?`;`session %q 用哪个 agent?(创建后不可更改)`→`Which agent for session %q? (permanent)`;`未检测到/已安装/(默认)`→`not found/installed/(default)`;`选择(序号或名称)`→`Choose (number or name)`;`无效选择`→`invalid choice` |
| `init.go` 启动失败 | `✗ %s 启动失败,claude 已退出`→`✗ %s failed to start (claude exited)`;`完整日志:`→`full log:` |
| `init.go` 重输密码 | `请重新输入注册密码`→`re-enter register password` |
| `main.go` 取消 | `已取消`→`cancelled` |

> 不在范围:`skills/undercover-dm/SKILL.md`(游戏内容,非 CLI plumbing)、CLAUDE.md InitTemplate(已英文)。

## 变更点

| 文件 | 改动 |
|------|------|
| `pkg/cli/runtime/session.go` | `RunUninstall()` 去 purge;恒 best-effort 注销 + 必清本地 |
| `cmd/agentlink/main.go` | 去 `--purge`;usage 回 `agentlink uninstall`;`已取消`→英文 |
| `install.sh` | 结尾指 `agentlink init` |
| `pkg/cli/runtime/init.go` | 收尾提示改写;启动失败/日志文案英文 |
| `pkg/cli/runtime/init_wizard.go` | 整个向导英文 |
| `pkg/cli/net/config.go` | `config file not found` 追加 `; run 'agentlink init' first` |
| `pkg/cli/net/messages.go` | send 回执/状态行英文 |
| `pkg/cli/runtime/poller.go` | 注入前缀/任务指引英文 |
| 测试 | uninstall(默认注销 + best-effort);相关文案断言 |

## 不做
- 不动 `#38` 已正确的部分(`init --force` 已删、server force 死分支已删)。
- 不改游戏 skill 语言。

## 验收
- `agentlink uninstall`(无 flag)→ 注销 server + 清干净本地;server 不可达 → **仍清干净本地**,只 warning;无 `--purge` 残留;`list` 不再有幽灵设备。
- `install.sh` 结尾指 `agentlink init`(裸)。
- `init` 完成提示不再有被自动 attach 盖掉的死行。
- 未 init 时任意命令报错带 `run 'agentlink init' first`。
- 全 CLI 输出无中文(`grep -rP '[\x{4e00}-\x{9fff}]'` 在 pkg/cli+cmd 的输出串里为空,注释除外)。
- 纯 CLI 改动,**server 无需重新部署**(注销走既有 `DELETE /agents/device`);server + CLI 编译测试通过。
