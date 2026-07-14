# 36 — `agentlink version` + 安装后打印版本号

Type: AFK

## 背景

线上排查问题时反复卡在一件事:**没法确认某台机器上装的是哪个版本**。`agentlink version` 目前报 `unknown command`,二进制里也没有嵌入任何版本信息。一键安装(install.sh)从 GitHub Release 下载预编译包,但**装完不打印版本**,用户也无从得知拿到的是不是最新的。

实际后果:多次"修复已发布,但用户机器还是旧行为"的困惑(例如 tmux 前缀匹配、root 下 IS_SANDBOX),本质都是**版本不可见** —— 没法一眼判断"这台机器该不该重装"。

## 现状

- `Makefile` 的 `build` / `build-all` **没有任何 `-ldflags -X` 版本注入**,版本信息完全没进二进制。
- `cmd/agentlink/main.go` 的命令 switch 里**没有 `version`**。
- `install.sh` 从 `releases/latest/download/...`(或指定 `$VERSION`)下载,末尾只打印 `✓ agentlink installed to ...`,**不显示版本**;"latest" 分支甚至没有解析出实际 tag。
- CI(`.github/workflows/ci.yml`)的 `release` job 在 `v*` tag 上跑 `make build-all` 上传产物 —— 有现成的 tag(`github.ref_name`)可注入,但目前没用上。

## 方案

### 1. 版本嵌入(编译期 ldflags)

- `cmd/agentlink/main.go` 增 `var version = "dev"`(默认 dev,便于本地 `make build`)。可选再加 `var commit = ""` / `var date = ""`。
- `Makefile`:引入 `VERSION`(优先取传入值,否则 `git describe --tags --always --dirty` 兜底),给 `build` 和 `build-all` 都加
  `-ldflags "-X main.version=$(VERSION)"`(如加 commit/date 则一并 `-X`)。
- CI `release` job:`make build-all VERSION=${{ github.ref_name }}`。release 只在 `v*` tag 触发,`ref_name` 即 tag(如 `v0.3.6`),正好作为版本号。

### 2. `agentlink version` 命令

- main.go switch 增 `case "version"`(并接受 `--version` / `-v`),打印:
  ```
  agentlink <version>
  ```
  若嵌入了 commit/date,追加一行。
- 归类:任何人可跑(不属于 User-only 也不属于 agent-only),`printUsage` 里补一行 `agentlink version`。

### 3. install.sh 装完打印版本

- 安装成功(现有 `✓ agentlink installed to ...` 之后),调用刚装好的二进制拿版本并打印:
  ```
  installed_version="$("$BINDIR/$BINARY" version 2>/dev/null || echo unknown)"
  echo "  version: $installed_version"
  ```
- 这样做一举两得:① 显示真实拿到的版本(比 `$VERSION="latest"` 更可靠);② **顺带当一次冒烟测试** —— 若二进制跑不起来(如 glibc 不匹配),`version` 失败会显示 `unknown`,当场暴露问题,而不是等到 init 才发现。

## 不做 / 边界

- 不引入第三方版本库,纯 ldflags + stdlib。
- 不改 server。
- 不做自动更新检查 / "有新版本"提示(可作后续)。

## 验收

- `make build` 产物 `agentlink version` 打印非空(本地为 `dev` 或 `git describe` 值)。
- CI 在 `v0.3.x` tag 上构建的 release 二进制,`agentlink version` 打印该 tag。
- `agentlink --version` / `-v` 等价于 `agentlink version`。
- `install.sh` 执行末尾打印 `version: <实际版本>`;二进制不可运行时显示 `unknown`。
- `printUsage` 含 `agentlink version`。
