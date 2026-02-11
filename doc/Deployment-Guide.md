# Deployment Guide

## Quick Start

1. Download or run DMRHub (see options below)
2. On first launch, the **setup wizard** opens automatically in your browser
3. Follow the wizard to configure the application and create your first admin user
4. DMRHub restarts into normal operation — you're done!

For advanced configuration options, see the [Configuration Guide](Configuration.md).

## Prerequisites

DMRHub ships with SQLite as the default database, so **no external services are required** for a basic deployment. Optionally, you can use:

- **PostgreSQL** or **MySQL** as the database backend (for larger or multi-server deployments)
- **Redis** for distributed pub/sub (for multi-instance scaling)

See the [Configuration Guide](Configuration.md) for database setup instructions.

## Running DMRHub

### Docker (Recommended)

Docker images are published at [ghcr.io/usa-reddragon/dmrhub](https://github.com/USA-RedDragon/DMRHub/pkgs/container/dmrhub).

Supported architectures: `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `linux/arm/v6`.

Tags follow Git releases. For example, `v1.0.4` produces tags `:v1.0.4`, `:1.0.4`, `:1.0`, and `:1`. Using a less specific tag like `:1` lets you receive updates automatically.

#### Basic Docker Run

```bash
sudo docker run \
    -d \
    --restart unless-stopped \
    -p 3005:3005 \
    -p 62031:62031/udp \
    -v dmrhub-data:/data \
    --name dmrhub \
    ghcr.io/usa-reddragon/dmrhub:1
```

On first run, open `http://localhost:3005` in your browser to complete the setup wizard.

#### Docker with YAML Config

To provide a pre-configured YAML file:

```bash
sudo docker run \
    -d \
    --restart unless-stopped \
    -p 3005:3005 \
    -p 62031:62031/udp \
    -v /path/to/config.yaml:/config.yaml:ro \
    -v dmrhub-data:/data \
    --name dmrhub \
    ghcr.io/usa-reddragon/dmrhub:1
```

#### Docker with Environment Variables

```bash
sudo docker run \
    -d \
    --restart unless-stopped \
    -e SECRET=your-random-secret \
    -e PASSWORD_SALT=your-random-salt \
    -e HTTP_CANONICAL_HOST=https://dmrhub.example.com \
    -p 3005:3005 \
    -p 62031:62031/udp \
    -v dmrhub-data:/data \
    --name dmrhub \
    ghcr.io/usa-reddragon/dmrhub:1
```

To adjust port mappings, change the host-side port number (before the `:`).

### Binary Releases

Binary releases are available at [GitHub Releases](https://github.com/USA-RedDragon/DMRHub/releases).

Binaries are released for many platforms including:

- linux/amd64, linux/arm64, linux/arm, linux/386, linux/riscv64
- darwin/amd64, darwin/arm64
- windows/386, windows/amd64
- freebsd, openbsd, netbsd variants

Only Linux is officially supported. On other platforms, ensure the configuration is provided appropriately.

#### Running on Linux with SystemD

##### 1. Create a System User

```bash
sudo mkdir /etc/dmrhub
sudo groupadd --system dmrhub
sudo useradd --home-dir /etc/dmrhub --no-create-home --no-user-group \
    --system --shell /sbin/nologin dmrhub
sudo chown dmrhub:dmrhub /etc/dmrhub
sudo chmod 770 /etc/dmrhub
```

##### 2. Install the Binary

```bash
sudo mv DMRHub /usr/local/bin/
sudo chmod a+x /usr/local/bin/DMRHub
```

##### 3. Create Configuration

Place a `config.yaml` in `/etc/dmrhub/`:

```bash
sudo -u dmrhub nano /etc/dmrhub/config.yaml
```

See the [Configuration Guide](Configuration.md) for the full reference. At minimum, you need:

```yaml
secret: "your-random-secret-here"
password-salt: "your-random-salt-here"
http:
  canonical-host: "https://dmrhub.example.com"
```

Alternatively, leave the config empty and use the setup wizard on first launch.

##### 4. Install the SystemD Unit File

The repository includes a SystemD unit file at [`hack/dmrhub.service`](../hack/dmrhub.service).

```bash
sudo cp hack/dmrhub.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now dmrhub.service
```

##### 5. Viewing Logs

```bash
journalctl -f -u dmrhub.service
```

## Ports

| Port  | Protocol |               Purpose                |
| ----- | -------- | ------------------------------------ |
| 3005  | TCP      | HTTP server (web UI, API, WebSocket) |
| 62031 | UDP      | MMDVM DMR server                     |
| 62035 | UDP      | OpenBridge server (if enabled)       |
| 9000  | TCP      | Metrics server (if enabled)          |
| 6060  | TCP      | PProf server (if enabled)            |

## Network Requirements

- MMDVM repeaters/hotspots connect via UDP to the DMR port (default 62031)
- Users access the web interface via HTTP on the configured port (default 3005)
- If deploying behind a reverse proxy, set `http.trusted-proxies` and `http.canonical-host` appropriately
