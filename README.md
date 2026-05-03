# agentlink

Cross-device CLI messaging for multi-agent Claude Code teams.

## Deploy the Server

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

Creates `main/` and `worker/` directories, each with `.agentlink.toml` + `CLAUDE.md`. Starts two tmux sessions (`main` and `worker`) running Claude Code, plus a background poller process for each.

### Messages

```bash
agentlink send <target> <content>        # send
agentlink pull [--all]                    # receive
```

### Tasks

```bash
agentlink task send <target> <id> "<content>"    # assign
agentlink task result <id> <status> "<result>"    # report
agentlink task resume <id> "<guidance>"           # resume
agentlink task cancel <id>                        # cancel
agentlink task status <id>                        # check
```

### Device & Sessions

```bash
agentlink ping                    # heartbeat (mark online)
agentlink list [--all]            # list devices
agentlink session add|remove <n>  # manage sessions
agentlink attach <session>        # enter a session
agentlink device remove           # unregister
agentlink poll                    # run poller in foreground
```

### Auto-Polling

Each session has a background poller started by `init`:

- Polls inbox every 5 seconds
- Injects new messages automatically when Claude is idle
- Skips when Claude is busy (generating / user is typing)
- Silently skips when pane capture fails

## Architecture

```
┌─────────┐     ┌──────────────┐     ┌────────┐
│  CLI    │────▶│  API Server  │────▶│  Redis │
└─────────┘     └──────────────┘     └────────┘
                     │
               ┌────┴────┐
               │  tmux   │
               └─────────┘
```

- **Server**: Go net/http + Redis (message queue, task storage, device registry)
- **CLI**: HTTP API for messaging, tmux for Claude interaction
- **Poller**: Background loop that checks for new messages and injects them when Claude is idle
- **Auth**: API key (SHA256 index) + Bearer Token
