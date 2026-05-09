# Server Deployment Guide

## Prerequisites

- A Linux server (this guide covers CentOS/OpenCloudOS/RHEL)
- A registered domain (recommended for HTTPS)

## Step 1: Install Go

```bash
wget -qO- https://go.dev/dl/go1.24.linux-amd64.tar.gz | tar -xz -C /usr/local
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

## Step 2: Build from source

```bash
git clone git@github.com:paparship/agent-link.git
cd agent-link
go build -o server ./cmd/server/
go build -o agentlink ./cmd/agentlink/
```

## Step 3: Install Redis

```bash
yum install -y redis
systemctl start redis
systemctl enable redis
```

Verify Redis is listening on localhost only:

```bash
grep bind /etc/redis.conf
```

## Step 4: Start the server

```bash
export REDIS_ADDR=localhost:6379
export REGISTER_PASSWORD=<your-registration-password>
export LISTEN_ADDR=:8080

./server
```

Test that it works:

```bash
curl http://localhost:8080/health
# → ok
```

Run in background:

```bash
nohup ./server > server.log 2>&1 &
```

## Step 5: Configure HTTPS (recommended)

### Option A: Caddy (auto HTTPS, recommended)

Install Caddy:

```bash
yum install -y yum-utils
yum-config-manager --add-repo https://caddyserver.com/rpm/caddy.repo
yum install -y caddy
```

Edit `/etc/caddy/Caddyfile`:

```
your-domain.com {
    reverse_proxy localhost:8080
}
```

Start Caddy:

```bash
systemctl start caddy
systemctl enable caddy
```

### Option B: nginx + Let's Encrypt

```bash
yum install -y nginx certbot python3-certbot-nginx
```

Edit `/etc/nginx/nginx.conf`:

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}

server {
    listen 80;
    server_name your-domain.com;
    return 301 https://$server_name$request_uri;
}
```

Obtain certificate:

```bash
certbot --nginx -d your-domain.com
```

## Step 6: Firewall

Open ports in the cloud provider's security group:

| Port | Purpose |
|------|---------|
| `443` | HTTPS (with Caddy/nginx) |
| `80` | HTTP redirect (with Caddy/nginx) |
| `8080` | Direct HTTP (no TLS, for testing only) |

Do **not** expose Redis (6379) to the internet.

## Step 7: Register devices

From any client machine:

```bash
agentlink init --server http://server-ip:8080 --password <password>
```

If HTTPS is set up:

```bash
agentlink init --server https://your-domain.com --password <password>
```

## Environment variables reference

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Server listen address |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REGISTER_PASSWORD` | (required) | Password for device registration |

`REGISTER_PASSWORD` can be unset after all devices are registered — registration is one-time.

## Removal

To completely remove the server from the machine:

```bash
# 1. Stop the server (cleans PID files)
cd /home/jiefan/agent-link/deploy && bash stop.sh

# 2. Delete agentlink Redis data only (does not affect other services)
redis-cli KEYS "agentlink:*" | xargs -r redis-cli DEL

# 3. Delete binaries and logs
rm -f /home/jiefan/agent-link/deploy/server
rm -f /home/jiefan/agent-link/deploy/server.log
rm -f /home/jiefan/agent-link/deploy/agentlink

# 4. (Optional) Delete the entire project
rm -rf /home/jiefan/agent-link
```

For a clean reinstall, steps 1-2 and replacing `deploy/server` are sufficient.
