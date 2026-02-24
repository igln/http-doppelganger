# GitLab Proxy

A Go reverse proxy application that forwards HTTP/HTTPS traffic and SSH connections to a self-hosted GitLab instance. Run this on a middle server that has access to your internal GitLab, allowing external users to access GitLab through the proxy.

## Features

- **HTTP Proxy**: Reverse proxy for GitLab web UI and REST API (port 80)
- **HTTPS with Let's Encrypt**: Automatic TLS certificate provisioning (port 443)
- **TLS Passthrough Mode**: Alternative mode to forward encrypted traffic without decryption
- **SSH Proxy**: TCP-level forwarding for Git operations over SSH (port 22)
- **URL Rewriting**: Automatically rewrites GitLab's internal URLs to your proxy domain
- **WebSocket Support**: Full support for GitLab's real-time features
- **Health Check Endpoint**: `/health` endpoint for monitoring
- **Graceful Shutdown**: Clean connection handling on termination
- **Docker Ready**: Production-ready Docker setup with automatic certificate renewal

## Architecture

```
Users                    Proxy Server                  GitLab (Internal)
─────                    ────────────                  ─────────────────
Browser    ──HTTP──►    :80   ──────────────────►    :80
Git Client ──HTTPS──►   :443  ──TLS Termination──►   :80
SSH Client ──SSH──►     :22   ──TCP Forward──────►   :22
```

## Quick Start (Docker with Let's Encrypt)

### Prerequisites

- Docker and Docker Compose installed
- A domain name pointing to your server (e.g., `gitlab.example.com`)
- Ports 80, 443, and 22 available
- Network access from proxy server to your internal GitLab

### 1. Clone and Configure

```bash
git clone https://github.com/http-doppelganger.git
cd http-doppelganger

# Copy environment file
cp .env.example .env
```

Edit `.env` with your domain and email:

```bash
DOMAIN=gitlab.example.com
EMAIL=admin@example.com
```

### 2. Configure GitLab Target

Edit `config.yaml`:

```yaml
gitlab:
  # Your internal GitLab server
  host: "gitlab.internal.example.com"
  http_port: 80
  https_port: 443
  ssh_port: 22
  
  # If GitLab redirects to a different URL, set it here
  # This enables URL rewriting in responses
  external_url: "https://gitlab.internal.example.com"
  
  # Additional URLs to rewrite (optional)
  rewrite_urls:
    - "http://gitlab.internal.example.com"

tls:
  enabled: true
  domain: "gitlab.example.com"
  cert_file: "/etc/letsencrypt/live/gitlab.example.com/fullchain.pem"
  key_file: "/etc/letsencrypt/live/gitlab.example.com/privkey.pem"
```

### 3. Initialize Let's Encrypt Certificate

```bash
# Set your domain and email
export DOMAIN=gitlab.example.com
export EMAIL=admin@example.com

# Run the initialization script
./scripts/init-letsencrypt.sh
```

This will:
1. Start a temporary nginx server for the ACME challenge
2. Request a certificate from Let's Encrypt
3. Update your `config.yaml` with TLS settings

### 4. Start the Proxy

```bash
docker-compose up -d
```

### 5. Verify

```bash
# Check logs
docker-compose logs -f gitlab-proxy

# Test health endpoint
curl http://localhost/health

# Test HTTPS
curl https://gitlab.example.com/health
```

## URL Rewriting

If your internal GitLab is configured with a different `external_url` than what users access through the proxy, you need URL rewriting.

**Example scenario:**
- Internal GitLab URL: `https://gitlab.internal.example.com`
- Public proxy URL: `https://gitlab.example.com`

GitLab generates redirects and links using its internal URL. The proxy rewrites these to your public domain.

**Configuration:**

```yaml
gitlab:
  host: "gitlab.internal.example.com"
  
  # GitLab's configured external_url
  external_url: "https://gitlab.internal.example.com"
  
  # Additional URLs to rewrite
  rewrite_urls:
    - "http://gitlab.internal.example.com"
    - "https://old-gitlab-url.example.com"

tls:
  domain: "gitlab.example.com"  # Your public proxy domain
```

The proxy will replace all occurrences of the internal URLs with `https://gitlab.example.com` in:
- HTTP `Location` headers (redirects)
- HTML responses
- JSON responses
- JavaScript files
- CSS files

## Manual Installation

### Build from source

```bash
go build -o gitlab-proxy ./cmd/proxy
```

### Run

```bash
# TLS passthrough mode (no certificates needed)
./gitlab-proxy -config config.yaml

# With Let's Encrypt certificates
# First, update config.yaml with tls.enabled: true
./gitlab-proxy -config config.yaml
```

**Note:** Running on ports 80, 443, 22 requires root privileges or capability settings.

## Configuration Reference

### Full Configuration Example

```yaml
gitlab:
  host: "gitlab.internal.example.com"
  http_port: 80
  https_port: 443
  ssh_port: 22
  external_url: "https://gitlab.internal.example.com"
  rewrite_urls:
    - "http://gitlab.internal.example.com"

proxy:
  http_listen: ":80"
  https_listen: ":443"
  ssh_listen: ":22"

tls:
  enabled: true
  cert_file: "/etc/letsencrypt/live/gitlab.example.com/fullchain.pem"
  key_file: "/etc/letsencrypt/live/gitlab.example.com/privkey.pem"
  domain: "gitlab.example.com"

logging:
  level: "info"
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `gitlab.host` | GitLab server hostname or IP | (required) |
| `gitlab.http_port` | GitLab HTTP port | 80 |
| `gitlab.https_port` | GitLab HTTPS port | 443 |
| `gitlab.ssh_port` | GitLab SSH port | 22 |
| `gitlab.external_url` | GitLab's external_url for URL rewriting | (empty) |
| `gitlab.rewrite_urls` | Additional URLs to rewrite | [] |
| `proxy.http_listen` | Proxy HTTP listen address | :80 |
| `proxy.https_listen` | Proxy HTTPS listen address | :443 |
| `proxy.ssh_listen` | Proxy SSH listen address | :22 |
| `tls.enabled` | Enable TLS termination | false |
| `tls.cert_file` | Path to TLS certificate | - |
| `tls.key_file` | Path to TLS private key | - |
| `tls.domain` | Domain name for the proxy | - |
| `logging.level` | Log level (debug/info/warn/error) | info |

### TLS Modes

**TLS Termination (recommended for Let's Encrypt)**
- Set `tls.enabled: true`
- The proxy decrypts HTTPS traffic and forwards HTTP to GitLab
- Requires valid certificates
- Enables URL rewriting in response bodies

**TLS Passthrough**
- Set `tls.enabled: false`
- The proxy forwards encrypted traffic directly to GitLab
- GitLab handles TLS termination
- URL rewriting only works for HTTP traffic

## Accessing GitLab through the Proxy

### Web UI
- HTTP: `http://gitlab.example.com`
- HTTPS: `https://gitlab.example.com`

### Git over HTTP/HTTPS
```bash
git clone https://gitlab.example.com/group/project.git
```

### Git over SSH
```bash
# Direct SSH (proxy listens on port 22)
git clone git@gitlab.example.com:group/project.git

# Or with explicit SSH URL
git clone ssh://git@gitlab.example.com/group/project.git
```

## Docker Commands

```bash
# Start
docker-compose up -d

# View logs
docker-compose logs -f gitlab-proxy

# Stop
docker-compose down

# Rebuild after code changes
docker-compose up -d --build

# Force certificate renewal
docker-compose exec certbot-renew certbot renew --force-renewal

# Clean up orphan containers
docker-compose down --remove-orphans
```

## Certificate Renewal

The `certbot-renew` service automatically checks for certificate renewal every 12 hours. Certificates are renewed 30 days before expiry.

To manually renew:

```bash
docker-compose exec certbot-renew certbot renew
```

After renewal, restart the proxy to load new certificates:

```bash
docker-compose restart gitlab-proxy
```

## Health Check

```bash
curl http://localhost/health
# Response: {"status":"healthy"}
```

## Running Without Docker

### systemd Service

Create `/etc/systemd/system/gitlab-proxy.service`:

```ini
[Unit]
Description=GitLab Proxy
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/gitlab-proxy
ExecStart=/opt/gitlab-proxy/gitlab-proxy -config /opt/gitlab-proxy/config.yaml
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable gitlab-proxy
sudo systemctl start gitlab-proxy
```

## Troubleshooting

### Redirects go to wrong domain

If clicking links redirects you to GitLab's internal URL instead of your proxy domain:

1. Set `gitlab.external_url` to GitLab's configured `external_url`
2. Add any other URLs GitLab might use to `gitlab.rewrite_urls`
3. Ensure `tls.domain` is set to your public proxy domain
4. Rebuild and restart the container

### Port 22 Conflict

If the host SSH daemon is using port 22:

```bash
# Option 1: Change host SSH to a different port
# Edit /etc/ssh/sshd_config, set Port 2222, then restart sshd

# Option 2: Use a different port for GitLab SSH proxy
# Edit config.yaml: ssh_listen: ":2222"
# Edit docker-compose.yml: ports: - "2222:22"
```

### Certificate Issues

```bash
# Check certificate status
docker-compose exec certbot-renew certbot certificates

# View certbot logs
docker-compose logs certbot-init

# Force renewal
docker-compose exec certbot-renew certbot renew --force-renewal
```

### Permission Denied on Port 80/443

```bash
# Allow binding to privileged ports
sudo setcap 'cap_net_bind_service=+ep' ./gitlab-proxy
```

### Container Won't Start

```bash
# Clean up and rebuild
docker-compose down --remove-orphans
docker-compose up -d --build
```

## Security Considerations

- The proxy passes traffic through without additional authentication by default
- Consider placing the proxy behind a firewall or VPN
- With TLS termination, the proxy sees decrypted traffic
- SSH connections are forwarded as raw TCP; the proxy doesn't validate SSH keys
- Keep Let's Encrypt certificates secure and regularly renewed
- URL rewriting modifies response content - test thoroughly

## License

MIT
