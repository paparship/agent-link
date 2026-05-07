# 3b — CLI send / pull

Type: AFK
Blocked by: 2b, 3a

## What to build

`agentlink send` 和 `agentlink pull` 命令的完整实现。

### 前置条件

CLI 从文件系统读取配置：
- `~/.agentlink/config.toml` → 获取 server、device
- `~/.agentlink/credentials.json` → 获取 api_key
- 当前 session 的 `.agentlink.toml` → 获取当前 session 名

任一文件缺失或解析失败 → 报错退出。

### agentlink send

```
agentlink send <target> <content>
```

- `<target>`：支持短名（`worker`）和完整名（`jiefan-pc:worker`）
- 短名自动补全：从 `~/.agentlink/config.toml` 读取 device 补全为 `device:session`
- `<content>`：消息内容，不能为空

**请求流程：**
1. 读取 config.toml 获取 server、device
2. 读取 credentials.json 获取 api_key
3. 读取当前目录（向上查找）`.agentlink.toml` 获取当前 session 名
4. 对短名 target 补全：如果不含冒号，补上 `device:`
5. `POST {server}/messages/send`，body `{ "to": "<full_target>", "from_session": "<current_session>", "content": "<content>" }`
6. 成功 → 打印 `✓ Message sent`
7. 失败 → 打印服务端错误信息，退出码 1

### agentlink pull

```
agentlink pull [--all]
```

- 无参数 → 拉取 1 条
- `--all` → 拉取最多 10 条（服务端 limit=10）

**请求流程：**
1. 读取 config.toml 获取 server、device
2. 读取 credentials.json 获取 api_key
3. 读取当前目录（向上查找）`.agentlink.toml` 获取当前 session 名
4. `GET {server}/inbox/pull?session=<session>&limit=<n>`
   - `--all` 时 `limit=10`，否则 `limit=1`
5. 有消息 → 逐条打印：

   ```
   [msg] from jiefan-pc:main — 2026-05-03T12:00:00Z
   hello
   ---
   ```

6. 无消息 → 打印 `No messages`，退出码 0
7. 失败 → 打印服务端错误信息，退出码 1

### 当前 session 查找规则

当前目录（或最近父目录）中查找 `.agentlink.toml`，读取 `session` 字段。

向上查找逻辑：从 cwd 开始，逐级向上找 `.agentlink.toml`，直到根目录。找不到则报错。

## Acceptance criteria

### agentlink send

- [ ] `agentlink send worker "hello"` → 成功发送，打印 `✓ Message sent`
- [ ] 短名 `worker` 自动补全为 `{device}:worker`
- [ ] 完整名 `jiefan-pc:worker` 不做补全
- [ ] 空 `content` 报错退出
- [ ] `content` 含空格和多行文本正常工作
- [ ] `config.toml` 不存在 → 报错退出
- [ ] `credentials.json` 不存在 → 报错退出
- [ ] `.agentlink.toml` 找不到 → 报错退出
- [ ] 服务端返回错误 → 打印服务端错误信息，退出码 1
- [ ] 网络不通 → 报错退出

### agentlink pull

- [ ] `agentlink pull` → 拉取 1 条，打印消息内容
- [ ] `agentlink pull --all` → 拉取最多 10 条，逐条打印
- [ ] 无消息时 → 打印 `No messages`，退出码 0
- [ ] 消息打印格式包含 from 身份、时间、内容、分隔线
- [ ] `config.toml` 不存在 → 报错退出
- [ ] `credentials.json` 不存在 → 报错退出
- [ ] `.agentlink.toml` 找不到 → 报错退出
- [ ] 服务端返回错误 → 打印错误，退出码 1

### 集成场景

- [ ] 同一设备：`send worker "hi"` → `pull`（在 worker session）→ 收到消息
- [ ] 跨设备：设备 A `send deviceB:worker "hi"` → 设备 B `pull` → 收到消息

## Blocked by

- Blocked by #2b
- Blocked by #3a
