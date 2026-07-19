# 37 — 消息投递不可靠:pull 即删、注入失败即丢(改为 ack-after-inject)

Type: BUG

## 现象(实测现场)

一局「谁是卧底」联机测试:Alice 发起,Mike、Bob 都回复了「加入」,但 **DM 只收到了 Mike 的加入,Bob 的丢了**。三方 agent 都活着、poller 都在跑。

Bob 侧确认发出成功:

```
● Bash(agentlink send jiefanlin-2l51jcfvsa:undercover-dm "bob 加入了，开搞！")
  ⎿  ✓ 消息已投递（ID: a97ff07931a3c8647d0afcac4b6ba6f8）
```

问题出在 DM 侧的 poller 日志:

```
message from ...:alice   → inject: ... ✓
message from ...:bob     → inject: bob 加入了，开搞！
send-keys error: exit status 1        ← Bob 这条注入失败
message from ...:mike    → inject: ... ✓
```

即:poller 把 Bob 这条**从服务器 inbox 拉了下来(拉取即删除)**,随后 `tmux send-keys` 注入失败(DM 正忙、TUI 那一刻拒键),**失败后只打了一行日志,消息已从服务器消失、无重投 → 永久丢失**。

**同一病根的另一张脸**:接收方 poller 活着、但它下面的 claude tmux(agent 进程)已死/卡住时,poller 一 pull 就把消息消费掉(标记已读),可消息**根本没进 agent**。

## 根因:pull 是破坏性弹出,且「从服务器删除」与「送达 agent」之间无原子性 / 无确认

一条消息从"服务器持久存储"到"进入 agent"分两步,而**第一步就把它删了**,第二步(注入)却可能失败/被跳过——中间任何闪失都丢消息。三个丢弃点:

1. **服务端 `pkg/api/handlers.go · handlePull`**:`data := s.rdb.RPop(inboxKey)` —— 拉取即弹出删除,没有"在途/待确认"暂存。
2. **poller `pkg/cli/runtime/poller.go · Run()`**:`if err := p.sendKeys(...); err != nil { 只打印 "send-keys error" }` —— 注入失败只记日志,消息已被 pop,不重投(Bob 的死法)。
3. **同上**:注入被 `!IsBusy && IsPromptEmpty` guard 跳过时(busy / capture 失败 / tmux 死),整块 inject 被跳过,而消息早已 pop —— **连日志都没有,更隐蔽**(tmux 死的那张脸)。

> 注:现有 `agentlink:current_msg:*` 键**只用于状态显示**("正在处理 msg: 标题(时长)"),不是持久/ack 机制。可靠投递需要新建。

## 方案:可靠队列 + ack-after-inject(at-least-once)

**原则:消息只有在「注入成功」后才从服务器删除。**

### 服务端

1. `handlePull`(poller 路径):把 `RPop(inbox)` 改为 **`RPOPLPUSH inbox → processing`**(原子预留到 per-session 的 `agentlink:processing:<device>:<session>` 槽),消息**留在 processing 不删**。
   - 每次 pull **先看 processing 里有没有上次没 ack 的在途消息**,有就**重投它**(不拉新的),从而覆盖"注入失败 / poller 崩 / tmux 死后重启"的重投。
2. 新增 **`POST /inbox/ack`**(`server.go` 注册一行路由 + `handleAck`):按 msg id 从 processing 清除。
3. `pull --all`(人工排空,limit>1)**保持现有 `RPop` 破坏式不变**;只给 poller 路径加可靠语义(用 `?reserve=1` 区分),把改动与风险圈在 poller 这一条线。

### 客户端(poller)

4. `Run()`:**注入成功才 `ack(msg.ID)`**;`sendKeys` 失败或 guard 跳过一律**不 ack** → 消息留在 processing → 下个 tick 自动重投。
5. **去重**(at-least-once 的代价:注入成功但 ack 丢失会导致重投=重复注入):poller 记 `lastInjectedID`,若 pull 回来的还是它 → 不重复注入,只补 ack。

## 变更点

| 位置 | 改动 |
|---|---|
| `pkg/api/handlers.go` `handlePull` | `RPop` → `RPOPLPUSH` + processing 在途重投(仅 reserve 路径) |
| `pkg/api/handlers.go` 新增 `handleAck` + `pkg/api/server.go` 路由 | `POST /inbox/ack` |
| `pkg/cli/runtime/poller.go` `Run()` | 成功才 ack + `lastInjectedID` 去重 |
| 测试 | 改 pull 测试(不再破坏式)+ 加 ack / 重投 / 去重测试 |

- 无 schema 迁移、无新依赖;新增一个 Redis key(`processing:*`)+ 一个端点。
- **需重新部署远端 server**(与以往纯 CLI patch 不同);CLI/poller 改动照旧出 patch。

## 不做 / 边界

- 不改 `pull --all` 的破坏式语义(人工排空场景 ack 意义不大)。
- 不引入外部消息队列;复用 Redis 原子操作即可。
- at-least-once,不追求 exactly-once;重复由客户端 `lastInjectedID` 去重兜住。

## 验收

- **Bob 的场景**:注入(send-keys)失败时消息不丢,下个 tick 重投直至注入成功。
- **tmux 死的场景**:接收方 agent 进程死/卡时,消息**不被消费掉**——留在 processing,agent 恢复(restart)后 pull 时重投送达。
- **poller 崩溃**:poller 进程重启后,processing 里未 ack 的消息被重投。
- **无重复**:注入成功但 ack 丢失时,重投不会二次注入(靠 `lastInjectedID`)。
- `pull --all` 行为与现状一致。
- server + CLI 编译、测试通过。
