# agentlink

Cross-device CLI messaging for multi-agent teams. Currently ships with Claude Code support.

## Quick Install (Linux / macOS)

```bash
curl -sfL https://github.com/paparship/agent-link/releases/latest/download/install.sh | sh
```

## Build from Source

```bash
make build              # build agentlink CLI
make build-server       # build server
make install            # build + install to /usr/local/bin
make uninstall          # remove from /usr/local/bin
make reinstall          # uninstall → build → install
make test               # run all tests
make clean              # remove build artifacts
```

Set `BINDIR` to override the install path:

```bash
make install BINDIR=~/.local/bin
```

## Deploy the Server

See [docs/deploy-server.md](docs/deploy-server.md) for the full systemd-based guide.

Requires Redis. Configure via environment variables:

```bash
export REDIS_ADDR=localhost:6379
export REGISTER_PASSWORD=<your-password>
export LISTEN_ADDR=:8080

./server
```

### Registration Flow

1. Start the server
2. Run `init` from any machine to register (needs server URL + password)
3. Server issues an API key — the device is now online

## CLI Usage

### Initialize a Team

```bash
agentlink init --server http://<server>:8080 --password <password> [./path]
```

Creates `main/` and `worker/` directories, each with `.agentlink.toml` + `CLAUDE.md`. Starts two tmux sessions (`main` and `worker`) running the configured agent (Claude Code by default), plus a background poller process for each.

### Messages

```bash
agentlink send [--interrupt] <target> <content>   # send (--interrupt wakes a busy agent)
agentlink pull [--all]                             # receive
agentlink message status <id>                      # check delivery status
```

`send` prints a recipient status panel showing whether the target is idle, busy (with current task + duration), or offline, plus the unread inbox depth. The message ID is shown on success for later status queries.

### Tasks

```bash
agentlink task send [--interrupt] <target> [<task_id>] "<content>"   # assign
agentlink task result <task_id> <status> "<result>"                   # report
agentlink task resume <task_id> "<guidance>"                          # resume
agentlink task cancel <task_id>                                       # cancel
agentlink task status <task_id>                                       # check
agentlink task list                                                   # list this device's tasks
```

### Device & Sessions

```bash
agentlink ping                    # heartbeat (mark online)
agentlink list [--all]            # list devices
agentlink session add|remove <n>  # manage sessions
agentlink attach <session>        # enter a session
agentlink resume                  # restore tmux + poller sessions after reboot
agentlink uninstall               # unregister device + clean up
agentlink poll                    # run poller in foreground
```

### Auto-Polling

Each session has a background poller started by `init`:

- Polls inbox every 5 seconds
- Injects new messages automatically when the agent is idle
- Injected messages carry a `[from device:session]` header so the agent can distinguish them from user input
- Sends a heartbeat every ~60s to keep the device marked online
- Auto-accepts the Claude Code trust prompt on first launch
- Skips when the agent is busy (generating / user is typing)
- Silently skips when pane capture fails

### Recovery After Reboot

`agentlink resume` rebuilds tmux sessions and pollers from the on-disk config, without re-registering the device. Run it after a machine restart:

```bash
agentlink resume
```

Each session's Claude Code is resumed to its last recorded `session_id` (saved in `~/.agentlink/config.toml` under `[sessions]`). For configs created before this feature, `--continue` is used as a fallback.

## Data Retention

Redis data is TTL-bounded to prevent unbounded growth:

| Data | TTL |
|------|-----|
| Inbox messages (unread) | 7 days |
| Delivered message records | 24 hours |
| Completed task records | 30 days |

## Architecture

```
┌─────────┐      ┌──────────────┐     ┌────────┐
│  CLI    │────▶│  API Server  │────▶│  Redis │
└─────────┘      └──────────────┘     └────────┘
                     │
                ┌────┴────┐
                │  tmux   │
                └─────────┘
```

- **Server**: Go net/http + Redis (message queue, task storage, device registry)
- **CLI**: HTTP API for messaging, tmux for agent interaction
- **Poller**: Background loop that checks for new messages and injects them when the agent is idle
- **Auth**: API key (SHA256 index) + Bearer Token
