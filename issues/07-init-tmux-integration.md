# 7 — init 集成 tmux 启动

Type: AFK
Blocked by: 2, 6

## What to build

`init` 命令的完整 tmux 集成，实现一行命令启动整个团队。

在现有 init 基础上增加：
- 前置检查：tmux 和 claude 命令是否存在，任一缺失报错退出
- 创建完文件和注册后，后台创建两个 tmux session：
  - `tmux new-session -d -s main -c {base_dir}/main 'claude'`
  - `tmux new-session -d -s worker -c {base_dir}/worker 'claude'`
- 输出提示：`agentlink attach worker` 切换到 worker
- `tmux attach -t main` 自动进入 main session
- 退出 Claude Code 后回到 shell

## Acceptance criteria

- [ ] tmux 不存在时 init 报错退出
- [ ] claude 不存在时 init 报错退出
- [ ] init 完成后 main 和 worker 的 tmux session 已在后台运行
- [ ] init 最后自动 attach 到 main session
- [ ] 退出 main 的 tmux session 后回到 shell

## Blocked by

- Blocked by #2
- Blocked by #6
