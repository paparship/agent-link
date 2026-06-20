# Server Deployment Guide

## Prerequisites

- Linux server with **root / sudo access**
- Go 1.24+, Redis 7+
- Port 8080 accessible from the outside (configure cloud firewall / security group)

## Step 1: Build

```bash
git clone https://github.com/paparship/agent-link.git
cd agent-link
go build -o server ./cmd/server/
go build -o agentlink ./cmd/agentlink/
```

## Step 2: Install binaries

```bash
sudo useradd -r -s /usr/sbin/nologin agent-link
sudo mkdir -p /opt/agent-link /etc/agent-link
sudo cp server /opt/agent-link/
sudo chown -R agent-link:agent-link /opt/agent-link
```

## Step 3: Configure Redis

Create `/etc/agent-link/redis.conf`:

```conf
bind 127.0.0.1
port 6379
dir /var/lib/agent-link/redis
maxmemory 256mb
maxmemory-policy allkeys-lru
```

```bash
sudo mkdir -p /var/lib/agent-link/redis
sudo chown agent-link:agent-link /var/lib/agent-link/redis
```

## Step 4: Create the environment file

Create `/etc/agent-link/server.env` (readable only by root):

```bash
sudo tee /etc/agent-link/server.env > /dev/null <<'EOF'
REGISTER_PASSWORD=<your-password>
EOF
sudo chmod 600 /etc/agent-link/server.env
```

## Step 5: Install systemd units

```bash
sudo cp deploy/agent-link-server.service /etc/systemd/system/
sudo cp deploy/redis.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now redis
sudo systemctl enable --now agent-link-server
```

Verify:

```bash
curl http://localhost:8080/health
# → {"ok":true,"redis":"connected"}
```

## Step 6: Register devices

From any client machine:

```bash
agentlink init --server http://<server-ip>:8080 --password <password>
```

Or over HTTPS (after configuring Caddy/nginx):

```bash
agentlink init --server https://your-domain.com --password <password>
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Server listen address |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REGISTER_PASSWORD` | (required) | Password for device registration — set in `/etc/agent-link/server.env` |

To change the password, edit `/etc/agent-link/server.env` and run `sudo systemctl restart agent-link-server`.

## Removal

```bash
# 1. Stop and disable services
sudo systemctl disable --now agent-link-server redis

# 2. Remove systemd units
sudo rm /etc/systemd/system/agent-link-server.service /etc/systemd/system/redis.service
sudo systemctl daemon-reload

# 3. Delete Redis data (agentlink keys only — safe for shared Redis)
redis-cli KEYS "agentlink:*" | xargs -r redis-cli DEL

# 4. Delete binaries, config, and data
sudo rm -rf /opt/agent-link /etc/agent-link /var/lib/agent-link

# 5. (Optional) Remove the user and source tree
sudo userdel agent-link
rm -rf ~/agent-link
```

## Notes

- **Redis must bind to localhost** — do not expose port 6379 to the internet.
- **Lighthouse vs CVM**: Lighthouse uses **防火墙** (not 安全组) in the console to open ports. CVM uses **安全组**.
- **Logs**: `journalctl -u agent-link-server -f`
- **HTTPS**: Add Caddy or nginx as a reverse proxy for HTTPS. Caddy auto-provisions Let's Encrypt certificates.
