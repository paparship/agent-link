# 30 — session add 不把 session 名写入 config

Type: BUG
Status: CLOSED — 与 #29 同源。本文根因(`&& sid != ""` 空值即跳过)成立,但其前提"session_id 常常为空"的深层原因见 #29 的修正说明。统一由 **#34** 给出完整方案。

## 现象

`agentlink session add reviewer` 成功:
- server 上有 `reviewer`(PATCH /agents/sessions)
- tmux session 已创建
- `~/.claude/projects/` 下有项目目录

但 `~/.agentlink/config.toml` 的 `[sessions]` 段没有 `reviewer`:

```toml
[sessions]
main = ""
worker = ""
```

导致 `agentlink restart` 不知道 `reviewer` 存在,不会重建它。

## 根因

`RunSessionAdd` 末尾更新 config 的代码:

```go
if sid, ok := sessions[name]; ok && sid != "" {
    api.UpdateSessionID(configPath, name, sid)
}
```

条件 `sid != ""` 导致 **session_id 为空时就跳过 config 写入**。即便 session 名本身应该被记录。

而 session_id 常常为空(见 issue 29, `readClaudeSessionID` 读取失效),所以新加的 session 名永远进不了 config。

## 修复方向

- 去掉 `&& sid != ""` 条件, session 名无论如何都应该写进 config
- `UpdateSessionID` 应拆分为"确保 session 在 config 中存在"和"更新 session_id"两步
- restart 时应从 server 拉取 sessions 列表,弥补本地 config 不完整的情况
