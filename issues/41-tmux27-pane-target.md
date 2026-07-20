# 41 — 老版本 tmux(2.7)下 poller 用 `-t =session` 注入全失效(消息卡在 processing 永不投递)

Type: BUG

## 现象(实测现场)

一局「谁是卧底」联机:main 向 `undercover-dm` 发 "开一局谁是卧底,邀请 bob alice mike",发送方回 `✓ message delivered`,但 DM 一直没收到。

- DM 的 claude 是空闲的,停在 `❯` 就绪提示。
- `undercover-dm-poller` 面板里有 `message from jiefanlin:main`,但 **后面没有 `inject:` 行**。
- redis 里这条消息(id `b212d50f…`)躺在 `agentlink:processing:jiefanlin:undercover-dm` 里被 reserve 住(issue 37 的 reserve 生效了),但因为始终没 ack,永远卡在 processing。

也就是说:reserve 成功、pull 成功,唯独注入那一步从没发生。

## 根因

这台机器是 **tmux 2.7**。poller 在 issue 32 的收尾里给 tmux 目标加了 `=` 前缀(强制精确匹配,避免 `main` 前缀命中 `main-poller`)。但:

- `=name` 精确匹配语法对 **session 目标**(`has-session` / `kill-session` / `attach`)在 2.7 上是支持的;
- 对 **pane 目标**(`capture-pane` / `send-keys`)在 tmux < 3.0 上**不支持**,会直接报 `can't find pane`。

本机实测:

```
tmux capture-pane -p -t =undercover-dm   → 失败: can't find pane
tmux capture-pane -p -t undercover-dm    → 成功 (exit 0)
tmux has-session   -t =undercover-dm     → 成功 (exit 0)   # session 目标 = 没问题
```

poller 的注入分支是:

```go
pane, err := p.capturePane(p.Session)
if err == nil && !IsBusy(pane) && IsPromptEmpty(pane) { ...inject... ack... }
```

`capturePane` 用了 `-t =session`,在 2.7 上**每 tick 都报错** → `err != nil` → 整个注入块被跳过 → 消息永远只 reserve 不 inject、不 ack → 卡死在 processing。DM 其实一直在等输入。

受影响的 4 处全是 pane 目标:
- interrupt 路径的 `send-keys -t =session Escape`
- `tmuxCapturePane` 的 `capture-pane -p -t =session`(致命的一处)
- `tmuxSendKeys` 的 `send-keys -l -t =session <text>` 和 `send-keys -t =session Enter`

## 方案

poller 的这 4 处 pane 目标去掉 `=`,恢复为普通 `-t session`。跨版本都能用(2.7 和新版都支持),且:

- agent session 活着时,普通目标精确命中它(tmux 目标解析里精确名优先),注入正常;
- agent session 死了时,普通目标会前缀命中 `<session>-poller`(poller 自己的面板),**但那个面板永远不会出现 `❯` 提示** → `IsPromptEmpty` 为 false → 不会注入 → issue 32 想防的"自己注入自己"依然被挡住,只是改由 `IsPromptEmpty` 兜底,而不是靠 `=` 报错。

session 目标的 `=`(`RunAttach` / `launchSessions` 的 `has-session` / `kill-session` / `attach`)**保持不动** —— 那些在 2.7 上正常,且 `=` 能防 attach 到 `-poller` 面板。

## 验证

- `go build ./...`、`go test ./pkg/cli/...` 通过。
- 本机 tmux 2.7 上 `capture-pane -t undercover-dm` exit 0。
- 修复后重启 poller,卡在 processing 的消息被重投并成功注入(reserve→inject→ack)。
