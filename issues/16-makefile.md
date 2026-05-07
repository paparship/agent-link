# 16 — Makefile：开发期快速构建 + 安装 + 卸载

Type: AFK

## 背景

当前开发迭代需要手动执行 3-4 步：`go build` → 找 binary 位置 → cp 到 PATH → 测试 → 手动清理。早期开发阶段频繁卸载重装（#13 uninstall），没有自动化入口。

需要一个 Makefile 把开发期常用操作压缩成一条命令。

## 设计方案

### Makefile targets

```makefile
build:               # 编译 agentlink CLI
	$(GO) build -o agentlink ./cmd/agentlink/

build-server:        # 编译 server
	$(GO) build -o server ./cmd/server/

install: build       # 编译 + 安装到 /usr/local/bin
	cp agentlink /usr/local/bin/agentlink

uninstall:           # 从 /usr/local/bin 删除 binary
	rm -f /usr/local/bin/agentlink

reinstall: uninstall install  # 重装（卸载 → 编译 → 安装）
	# 组合 target

clean:               # 删除编译产物
	rm -f agentlink server

test:                # 运行全部测试
	$(GO) test ./... -count=1

lint:                # 代码检查（可选）
	$(GO) vet ./...
```

### 设计要点

- `GO ?= go` — 允许外部覆盖，适配不同 Go 版本
- `install` 依赖 `build`，一次完成编译+安装
- `reinstall` 依赖 `uninstall` + `install`，干净重装
- `BINDIR ?= /usr/local/bin` — 可通过环境变量覆盖安装目录

### 开发循环

```bash
make reinstall                     # 1秒：删除旧版 → 编译 → 装到 PATH
agentlink init --server ...        # 测试
agentlink uninstall                # 清理
```

## 涉及文件

| 文件 | 改动 |
|------|------|
| `Makefile` | **新增** |
| `README.md` | 补充 `make build` / `make install` 说明 |
| `.gitignore` | 追加 `agentlink` `server` 二进制产物 |

## 不做的

- 不替代 #15 的 install.sh（Makefile 面向开发者，install.sh 面向最终用户）
- 不做 GitHub Actions CI 配置（后续 issue）
- 不做跨平台交叉编译（后续需要时加 `GOOS`/`GOARCH`）

## Acceptance criteria

- [ ] `make build` 编译出 `agentlink` binary
- [ ] `make build-server` 编译出 `server` binary
- [ ] `make install` 编译 + cp 到 `$(BINDIR)`
- [ ] `make uninstall` 删除 `$(BINDIR)/agentlink`
- [ ] `make reinstall` 组合卸载+重装成功
- [ ] `make test` 运行全部测试通过
- [ ] `make clean` 删除编译产物
- [ ] `BINDIR` 可被环境变量覆盖

## Blocked by

- Blocked by #13（uninstall 命令做好后，开发循环才完整）
