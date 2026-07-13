# 31 — init 交互式向导

Type: AFK

## What to build

给 `agentlink init` 增加**交互式向导**:当在终端中运行且缺少必填参数时,逐项引导用户填写 `--server` / `--password` / `--device`,每项独立校验、失败当场重问,而不是报错退出整条重来。

目标场景:内部办公同事加入团队网络时,不必背一长串 flag,直接 `agentlink init` 回车,跟着提示走完即可。

### 设计决策(已定稿)

1. **触发方式**:缺 `--server` 或 `--password` 且 stdin 是终端(TTY)时,自动进入向导。flag 齐全走原非交互流程;非 TTY(管道 / CI)缺参仍按现在报错退出。
2. **交互深度**:只问 3 项 —— server / password / device。`path`(默认 `~/agentlink`)、`auto-poll`(默认开)、`agent`(默认 claude)不问,需要改用对应 flag 覆盖。
3. **零依赖**:不引入第三方交互库。密码隐藏输入用 `stty -echo` / `stty echo`(项目已大量用 `exec.Command` 驱动 tmux),TTY 判断用 stdlib(`os.Stdin.Stat()` 的 `os.ModeCharDevice` 位)。

### 命令签名(不变)

```
agentlink init --server <url> --password <pw> [--device <name>] [--agent claude] [--no-poll] [--force] [path]
```

所有 flag 保持向后兼容。flag 已提供的值 → 作为向导默认值,或直接跳过该项提问(例如 `init --server X` 只忘了密码时,向导只补密码)。

### 触发逻辑(`cmd/agentlink/main.go` `cmdInit`)

```
parse flag
  ↓
server 和 password 都齐全?
  ├─ 是 → 走现有非交互流程(自动化不受影响)
  └─ 否 → stdin 是终端?(os.Stdin.Stat().Mode() & os.ModeCharDevice != 0)
           ├─ 是 → 进入交互向导补全空缺项
           └─ 否 → 保持现在的报错退出(管道 / CI 不能挂起等输入)
```

### 向导问答项

| 顺序 | 项 | 默认值 | 校验 & 重试 |
|----|----|----|----|
| ① | Server URL | 已有 `~/.agentlink/config.toml` 的 `server`,否则空 | 补全 `http://` 前缀、去掉末尾 `/`;即时 `GET {server}/health` 探测,`{"ok":true}` 才通过,不通打印原因并重问 |
| ② | Password | 无 | `stty -echo` 隐藏输入;正确性在注册步校验(见下),401 时只重问密码 |
| ③ | Device 名 | `os.Hostname()` | 非空即可 |

- 每项最多重试 3 次,超过则打印提示并退出码 1,避免死循环。
- 向导任一步 Ctrl+C:此时尚未建目录、尚未注册,直接干净退出。

### 重排注册顺序(顺带的健壮性改进)

当前 `RunInit`(`pkg/cli/runtime/init.go`)顺序为「建目录 → 写 config → 注册 → 写凭据 → 写 session 文件 → 起 tmux」,密码错误时会残留半个已创建的目录。

改为**先注册、再落地本地文件**:

```
向导收齐 server / password / device
  ↓
调 registerDevice(纯网络请求,无本地副作用)
  ├─ 401(密码错) → 交互模式下只重问密码后重试;非交互模式按原样报错退出
  ├─ 其它网络错   → 交互模式下打印原因、回到 server 那步重填;非交互模式报错退出
  └─ 成功 → 拿到 api_key
  ↓
打印摘要(server / device / path / auto-poll / force),交互模式下 y/N 确认
  ↓
建目录 → 写 config / credentials / 各 session 文件 → 起 tmux → attach main
```

好处:注册失败不留残留目录;密码 / 地址填错都能当场纠正,无需重跑整条命令。

### 代码落点

- **新增** `pkg/cli/runtime/init_wizard.go`:
  - `promptInitOptions(*InitOptions) error` —— 补全 opts 中空缺的 server / password / device,含校验与重试
  - `promptLine(prompt, def string) string` —— 读一行,回车用默认值
  - `promptSecret(prompt string) string` —— `stty -echo` 包裹的隐藏输入
  - `promptConfirm(prompt string) bool` —— y/N 确认
  - `probeHealth(server string) error` —— `GET /health` 探测
  - `isInteractive() bool` —— TTY 判断
- **改** `cmd/agentlink/main.go` `cmdInit`:把「缺 server/password 即报错」替换为上面的触发逻辑分支。
- **微调** `pkg/cli/runtime/init.go` `RunInit`:将 `registerDevice` 调用提到建目录 / 写文件之前,把「注册」与「落地本地文件」拆成前后两段;密码错误在交互模式下可重试。

### 错误处理

- 非交互模式(flag 齐全或非 TTY)行为完全不变:注册失败、网络不通、目录已存在 → 报错退出码 1。
- 交互模式:server 不通 / 密码错 / 各类校验失败 → 打印原因并在对应步骤重问,不退出;超过重试上限才退出。
- `--force`:目录已存在时,在摘要确认步明确提示将覆盖。

## Acceptance criteria

- [ ] `agentlink init`(无任何参数)在终端中运行 → 进入向导,依次询问 server / password / device
- [ ] `agentlink init --server http://host:8080 --password xxx` → flag 齐全,走原非交互流程,行为不变
- [ ] `agentlink init --server http://host:8080`(缺密码)在终端 → 只询问 password,server 用已给值
- [ ] 向导中 server 地址不通 → 提示连接失败并重问,不退出
- [ ] 向导中 server 地址自动补全 `http://` 前缀、去掉末尾 `/`
- [ ] 向导中密码输入不在终端回显(`stty -echo` 生效)
- [ ] 向导中密码错误(server 返回 401)→ 只重问密码并重试注册,不重来整条命令
- [ ] 向导中 device 默认值为 hostname,直接回车即采用
- [ ] 注册失败时**不残留**已创建的工作目录
- [ ] 摘要确认步显示 server / device / path,输入 n → 中止,不做任何落地
- [ ] 非 TTY(管道 / 重定向 stdin)且缺 server/password → 保持原报错退出,不挂起
- [ ] 未引入任何新的 Go 第三方依赖(go.mod 不新增 require)
- [ ] 现有 `init_test.go`(flag 齐全路径)仍通过
