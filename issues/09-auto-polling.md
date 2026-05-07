# 9 — 自动轮询后台进程

Type: AFK
Blocked by: 2, 3, 4, 7

## What to build

后台进程定期轮询收件箱，检测到新项目且 Claude 空闲时注入 prompt。

**轮询逻辑：**
1. 每 N 秒调用 `GET /inbox/pull?limit=1`
2. 有项目则进入注入流程，无则继续轮询

**空闲检测逻辑（按顺序执行）：**
1. `tmux capture-pane -t <session> -p` 成功捕获 pane 内容 → 失败则跳过
2. pane 内容不含 `esc to interrupt` → 含则 Claude 在忙，跳过
3. 最后几行提示符（`❯`）独占行首 → 输入框为空

**注入方式：**
- `tmux send-keys -t <session> <content> Enter`
- 每次只注入 1 条
- 注入后等待 Claude 处理完（空闲检测通过），再拉取下一条

**配置：**
- 在 `init` 启动的 tmux session 中以后台进程运行
- 轮询频率可配置（默认 5 秒）

## Acceptance criteria

- [ ] 后台进程能拉到新消息
- [ ] Claude 忙时不注入（含 `esc to interrupt`）
- [ ] 输入框不为空时不注入
- [ ] Claude 空闲时自动注入新消息
- [ ] 一次只注入 1 条，回复完再拉下一条
- [ ] pane 无法捕获时静默跳过，不报错

## Blocked by

- Blocked by #2
- Blocked by #3
- Blocked by #4
- Blocked by #7
