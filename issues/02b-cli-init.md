# 2b — CLI init 命令

Type: AFK
Blocked by: 2a

## What to build

`agentlink init` 命令的完整实现。依赖 2a 服务端运行中。

### 命令签名

```
agentlink init --server <url> --password <pw> [--device <name>] [./path]
```

- `--server`：必填，API 服务端 URL
- `--password`：必填，注册密码
- `--device`：可选，设备名。省略时自动取 OS hostname（`os.Hostname()`）
- `[./path]`：可选，工作目录路径，默认 `./agent_team`

### 前置检查

- `tmux` 命令是否存在（`exec.LookPath("tmux")`），不存在则报错退出
- `claude` 命令是否存在，不存在则报错退出
- 任一缺失：
  ```
  Error: require "tmux" and "claude" to be installed
  ```

### 执行流程

1. 解析参数，device 留空时调用 `os.Hostname()` 获取
2. 创建工作目录（默认 `./agent_team/`）+ `main/`、`worker/` 子目录
3. 写入 `~/.agentlink/config.toml`：

   ```toml
   server = "https://msg.example.com"
   device = "jiefan-pc"
   base_dir = "/absolute/path/to/agent_team"
   ```

4. POST `{server}/agents/register`（请求体含 device、sessions=["main","worker"]、register_password）
5. 注册成功 → 将返回的 API Key 写入 `~/.agentlink/credentials.json`：

   ```json
   {
     "api_key": "sk_live_xxx",
     "registered_at": "2026-05-03T12:00:00Z"
   }
   ```

6. 写入 `main/.agentlink.toml`：

   ```toml
   session = "main"
   device = "jiefan-pc"
   ```

7. 写入 `worker/.agentlink.toml`：

   ```toml
   session = "worker"
   device = "jiefan-pc"
   ```

8. 输出成功提示：

   ```
   ✓ Agent team initialized at /path/to/agent_team
   ✓ Device "jiefan-pc" registered (sessions: main, worker)
   
   Next steps:
     agentlink attach worker    # 切换到 worker session
   ```

### 错误处理

- 注册 API 返回错误（密码错误、device 重复等）→ 打印服务端错误信息，退出码 1
- 网络不通 → 打印连接失败信息，退出码 1
- 目标目录已存在 → 报错退出，不覆盖

### 文件产出清单

| 路径 | 内容 |
|------|------|
| `~/.agentlink/config.toml` | server、device、base_dir |
| `~/.agentlink/credentials.json` | api_key、registered_at |
| `{dir}/main/.agentlink.toml` | session=main、device |
| `{dir}/worker/.agentlink.toml` | session=worker、device |

### 依赖

需要新增 Go 依赖：
- TOML 库（写 config.toml，推荐 `BurntSushi/toml`）
- 已有：`crypto/rand`、`crypto/sha256`（stdlib）

## Acceptance criteria

- [ ] `agentlink init --server http://localhost:8080 --password xxx` 成功执行
- [ ] `agentlink init --server http://localhost:8080 --password xxx --device my-pc` 使用自定义设备名
- [ ] 省略 `--device` 时自动使用 hostname
- [ ] 前置检查：tmux 不存在时报错退出
- [ ] 前置检查：claude 不存在时报错退出
- [ ] `~/.agentlink/` 目录及其下文件正确创建
- [ ] `~/.agentlink/config.toml` 包含 server、device、base_dir（绝对路径）
- [ ] `~/.agentlink/credentials.json` 包含 api_key（sk_live_ 开头）和 registered_at
- [ ] `agent_team/main/.agentlink.toml` 包含 session=main
- [ ] `agent_team/worker/.agentlink.toml` 包含 session=worker
- [ ] 服务端 Redis 有 `device:<hostname>` 记录
- [ ] 注册失败（密码错误）→ 报错退出
- [ ] 注册失败（device 重复）→ 报错退出
- [ ] 网络不通 → 报错退出
- [ ] 目标目录（默认 `./agent_team`）已存在时 → 报错退出
- [ ] `init` 执行完后，`credentials.json` 中的 API Key 可成功调用需要鉴权的 API

## Blocked by

- Blocked by #2a
