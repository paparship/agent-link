# 38 — init / uninstall 收口:去掉 `--force` 半替换、删 server force 死分支、补 uninstall 本地清理

Type: BUG

## 背景

对"安装 / 卸载 / 重装 / 更新"全链路做清洁度审计,发现几处**互相关联**的不干净点,集中在 `init --force` 的语义和 `uninstall` 的可靠性上。核心矛盾:**在已初始化的机器上重跑 `init` 会做"新旧混合的半替换",而 `uninstall` 又在 server 不可达时清不干净**——想干净重来反而没有可靠路径。

## 问题

**1. `init --force` 语义误导且危险**
- help 写的是 `Force re-register if device exists`,但它实际**既不 re-register、也和 device-exists 无关**。它唯一的作用是绕过 `RunInit` 里"目录已存在"的本地检查(`init.go`),让 init 在已存在的 `base_dir` 上**就地覆盖**:重写 `config.toml`/`credentials.json`、重建 main/worker,而自定义 session 目录等**残留** → **新旧混合的半替换状态**,危险且难察觉。

**2. server 端 register 有一段"死且危险"的 force 分支**
- `handleRegister` 里有 `if req.Force { deleteDeviceData(device) }`(删设备 + api_key + inbox + 任务)。但客户端 `registerRequest` **根本没有 force 字段、从不发** → 这是**死代码**。
- 更糟:`register_password` 是**全网共享的单一密钥**。一旦这条被接上,任何知道该密码的 client 就能 `init --device <别人> --force` 抹掉**别人设备**的注册/inbox/任务 → **"client 强制 server 删他人数据"**的隐患。

**3. `uninstall` 不可靠(issue 18 设计过但从未实现)**
- 顺序:先杀 tmux → 再 `DELETE /agents/device`。`APIDo` 对**任何非 200 / 传输错误都 return err**,于是 `RunUninstall` 直接返回,**本地 `base_dir` / `~/.agentlink` 不清理**。→ server 不可达或设备已注销时:**半清理,且永远卸不干净**。
- 附带:`uninstall` **不删二进制**(`/usr/local/bin/agentlink` 残留);结尾提示"改 ~/.bashrc 删 PATH",但 `install.sh` **从没动过 PATH** → 提示错位,真正的残留(二进制)反而没提。

## 方案

统一收口为:**`init` 只做首次引导;"重来" = `uninstall`(本地一定清干净)+ `init`;全程无 `--force`、无半替换、无 client-force-server。**

### 1. 去掉 `init --force`
- 删 `cmd/agentlink/main.go` `cmdInit` 的 `--force` flag 及 `InitOptions.Force`。
- `RunInit`:`base_dir` 已存在时**无条件明确报错**:
  `directory %q already exists; run 'agentlink uninstall' first`
  不再提供"就地覆盖"这条路。

### 2. 删 server 端 register 的 force 死分支
- `handleRegister`:移除 `req.Force` → `deleteDeviceData(...)` 分支;请求模型里的 `force` 字段一并去掉(客户端本就不发,老客户端即便带上也被 JSON 忽略,兼容)。
- **保留**已存在设备的默认幂等行为(校验共享密码 → 保留设备、轮换 key)不变。
- 注:今后若确需"清理 server 端某设备状态",应由**该设备自身的 api_key**(证明是本人)授权,而非"共享 register_password + flag"。本 issue 只删危险死分支,不实现新授权机制。

### 3. 按 issue 18 原设计补 `uninstall` 拆分
- `agentlink uninstall`:**只清本地**(杀 tmux、删 `base_dir`、删 `~/.agentlink`),**本地清理不受 server 影响**;尝试通知 server 若失败,只打 warning、不中断本地清理。
- `agentlink uninstall --purge`:本地清理 + `DELETE /agents/device`(即现有的完整行为)。
- 顺带:去掉误导的"改 rc 删 PATH"提示;能删二进制则一并删(或至少明确打印二进制真实路径,让用户自行删)。

## 变更点

| 文件 | 改动 |
|------|------|
| `cmd/agentlink/main.go` | 删 `init --force`;`uninstall` 加 `--purge` |
| `pkg/cli/runtime/init.go` | 删 `Force`;目录已存在 → 无条件报错(提示先 uninstall) |
| `pkg/cli/runtime/session.go` | `RunUninstall`:本地清理优先且必成;server 注销移到 `--purge`;修提示、删二进制 |
| `pkg/api/handlers.go` | 删 `handleRegister` 的 `force`/`deleteDeviceData` 分支 |
| `pkg/api/`(模型) | 删注册请求的 `force` 字段 |
| 测试 | 更新 init / uninstall / register 相关测试 |

## 不做

- 不引入账号层或按设备独立密码。
- 不实现"授权式 server 端清理"(仅删危险死分支)。
- `deleteDeviceData` 函数本身保留(`--purge` 的 `DELETE /agents/device` 仍会用到);只是不再从 register 的 force 路径触发。

## 验收

- `init` 在已存在 `base_dir` 上 → **明确报错并提示先 `uninstall`**,不再半覆盖;全新目录 → 正常。
- 代码中无 `--force`(client)/ `req.Force`(server)残留。
- **server 不可达时 `agentlink uninstall` 仍能把本地清干净**;`--purge` 才尝试注销 server。
- 同名设备"卸载→重装"(`uninstall` → `init`)链路顺畅。
- `agentlink uninstall` 后 `/usr/local/bin/agentlink` 被删(或明确提示其路径);不再出现无意义的 PATH 提示。
- server + CLI 编译、测试通过。**改了 `handleRegister` 与请求模型,需重新部署 server。**
