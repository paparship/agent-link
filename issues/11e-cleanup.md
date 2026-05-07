# 11e — 清理收尾

Type: AFK
Blocked by: 11b, 11c, 11d

## What to build

前面 4 个 slice 改完后，验证没有残留的 claude 硬编码泄漏到 core 包中。

**搜索验证：**
```bash
grep -rn -i "claude" pkg/cli/ pkg/api/
```

预期只剩 `config.toml` 中的 `agent = "claude"` 默认值，和 `adapter.NewLauncher("claude")` 调用处的字符串字面量。

**测试全面运行：**
```bash
go test ./... -count=1
```

**检查是否还有未搬移的旧函数：**
- `checkPrereqs` — 应在 11b 中删除 ✓
- `claudeMDContent` — 应在 11b 中删除 ✓
- `IsClaudeIdle` — 应在 11c 中删除 ✓
- `checkTmux` — 检查是否被 attach 使用，若仅被 session.go 使用则保留或迁移

## Acceptance criteria

- [ ] `pkg/cli/` 和 `pkg/api/` 中无 Claude Code 特定常量或逻辑
- [ ] `grep -rn -i "claude" pkg/*/` 只命中 adapter 包和 config 默认值
- [ ] `go test ./... -count=1` 全部通过
- [ ] 已删除 `IsClaudeIdle`、`claudeMDContent`、`checkPrereqs` 旧函数

## Blocked by

- Blocked by #11b, #11c, #11d
