# 15 — CLI 一键安装：GitHub Releases + install.sh

Type: AFK

## 背景

当前用户拿到 `agentlink` binary 后无法直接使用——binary 不在 PATH 中，也没有任何安装步骤。文档和 help 里所有命令示例都假设 `agentlink` 已在 PATH，但这个假设从未被满足。

**用户现状：**
1. `git clone` 或 `scp` 拿到 binary
2. 不知道放哪，只能在项目目录用 `./agentlink`
3. 没有 `go install` 或 `make install` 之类入口

**目标：** 一行命令完成安装，不依赖 Go、不依赖任何包管理器。

## 设计方案

### 1. GitHub Releases 发布预编译二进制

每次 tag 发布时自动构建并上传：

| 平台 | 文件名 |
|------|--------|
| Linux amd64 | `agentlink-linux-amd64` |
| Linux arm64 | `agentlink-linux-arm64` |
| macOS amd64 | `agentlink-darwin-amd64` |
| macOS arm64 | `agentlink-darwin-arm64` |

第一次手动发布，后续接入 GitHub Actions 自动构建。

### 2. install.sh

放在 repo 根目录，也上传到 Release assets。

**流程：**

```
1. 检测 OS (linux/darwin) + ARCH (amd64/arm64)
2. 拼出下载 URL → GitHub Releases latest
3. 下载 binary → 校验（可选）
4. chmod +x
5. 安装到 /usr/local/bin（需要 sudo）或 ~/.local/bin（fallback）
6. 提示安装成功
```

**用户安装命令：**

```bash
curl -sfL https://github.com/paparship/agent-link/releases/latest/download/install.sh | sh
```

### 3. 可选：`agentlink upgrade` 自更新

设计上预留但不先实现。`install.sh` 加 `--version` 参数即可覆盖安装。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `install.sh` | **新增** — 安装脚本 |
| `README.md` | 新增安装说明：一行命令安装 |
| `README_zh.md` | 同上 |
| `docs/deploy-server.md` | 服务端部署文档中补充客户端安装指引 |

## 不做的

- 不实现 `agentlink upgrade`（后续 issue）
- 不接入 GitHub Actions（先手动发布一次，后面再补 CI）
- 不放签名/哈希校验（第一版先做通路，安全加固后续加）
- 不支持 Windows（当前只有 Linux/macOS 用例）

## Acceptance criteria

- [ ] `install.sh` 自动检测 linux/darwin + amd64/arm64
- [ ] `install.sh` 从 GitHub Releases 下载对应 binary
- [ ] `install.sh` 优先写到 `/usr/local/bin`，失败 fallback `~/.local/bin`
- [ ] `install.sh` 执行后 `agentlink --help` 可用
- [ ] GitHub Release 包含上述 4 个平台的 binary
- [ ] README / README_zh / docs 更新安装说明
- [ ] 从全新环境运行 `curl ... | sh` 一次成功

## Blocked by

None — 需要先手动创建第一个 GitHub Release
