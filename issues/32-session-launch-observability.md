# 32 — session 启动失败被静默掩盖 + 缺乏可观测性

Type: BUG

## 现象

在某台机器上 `agentlink restart` 后,`agentlink attach main` 进入的**不是 main 会话,而是空白的 `main-poller` 窗口**(worker 同理)。反复 restart 无效。日志区看不到任何失败原因,只能靠猜。

真实复现环境是一个 root 环境,且 init 首次是成功的——所以并非某个固定原因,而是"claude 某次没起来,但系统没有告诉任何人"。

## 根因(两层叠加)

**第一层:tmux `-t` 前缀匹配把故障伪装成"进错窗口"**

`RunAttach`(`pkg/cli/runtime/session.go`)用的是:

```go
hasSession := exec.Command("tmux", "has-session", "-t", session).Run() == nil  // -t main
cmd := exec.Command("tmux", "attach", "-t", session)                           // -t main
```

tmux 的 `-t` 目标解析顺序是:精确 → **前缀** → fnmatch。当精确的 `main` 会话不存在、但 `main-poller` 存在时,`has-session -t main` 和 `attach -t main` 都会**前缀匹配到 `main-poller`**,于是:
- 把"main 不存在"这个错误,变成"判定存在并 attach 到了 poller"
- 用户进入空白 poller 窗口,毫无线索

同样,`kill-session -t main`(init.go / session.go)在 main 不存在时也会误命中 `main-poller`。

**第二层:claude 秒退,报错随 tmux 会话蒸发**

`launchSessions`(`pkg/cli/runtime/init.go`)用 `tmux new-session -d -s main <claude ...>` 以 detached 方式起 claude。一旦 claude 启动即退出(root 拒绝 `--dangerously-skip-permissions`、`--continue` 找不到/无法恢复会话、config 损坏、版本不兼容……):
- 前台进程退出 → `main` 会话随即自动关闭 → 只剩 `main-poller`
- claude 退出前的 stderr / 最后一屏**没有任何地方留存**,退出码也丢了

两层叠加:一个本可明确报错的启动失败,变成了"莫名其妙进了空窗口",无法排查。

## 修复方向

**① tmux 目标一律精确匹配**
`has-session` / `attach` / `kill-session` 的 `-t` 参数改用 `=` 前缀强制精确匹配(`-t =main`),`main` 不存在时如实报错,而不是退化到 `main-poller`。

**② 启动 claude 时把 stderr + 退出码 + 时间落日志**
用 wrapper 启动:`sh -c '<claude ...> 2>><logfile>; echo "[exited code=$? at <ts>]" >><logfile>'`,日志写到 `~/.agentlink/logs/<session>.log`。stderr 单独重定向到文件,不干扰 TUI 的 stdout,正常运行时日志基本为空,失败时留下报错原文与退出码。

**③ 启动后做存活探测 + 失败报告**
`new-session` 后短暂等待,用 `has-session -t =<session>`(精确)判断会话是否还活着。若已死亡,判定为启动失败,读取该 session 日志尾部,打印:

```
✗ main 启动失败,claude 已退出
  <日志尾部报错>
  完整日志: ~/.agentlink/logs/main.log
```

而不是继续假装成功、去起 poller。

**④(可选后续)capture-pane 抓最后一屏**
若需要抓那些只渲染在 TUI 屏幕、没走 stderr 的报错,可对会话设 `remain-on-exit on` 并在死亡时 `capture-pane -p -t =<session>`。代价是要管理 dead pane(后续 kill-session 清理),故列为后续增强,本 issue 先落 ①②③。

## 影响范围

纯 CLI(`pkg/cli/runtime`),不涉及 server。

## Acceptance criteria

- [ ] `attach`/`has-session`/`kill-session` 均使用 `-t =<name>` 精确匹配
- [ ] `main` 会话不存在时,`agentlink attach main` **不再**进入 `main-poller`,而是明确报错或按 new-session 分支重新拉起
- [ ] 每个会话的 claude stderr + 退出码 + 时间写入 `~/.agentlink/logs/<session>.log`
- [ ] `launchSessions`(init 与 restart 共用)在会话启动后做存活探测,claude 秒退时打印失败原因与日志路径
- [ ] claude 正常起来时,启动流程与输出与原先一致(日志静默、无多余噪音)
- [ ] 未引入新的第三方依赖;现有测试通过
