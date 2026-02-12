# Configuration Guide

## Setup Wizard (Recommended)

The easiest way to configure DMRHub is with the **built-in setup wizard**. When DMRHub starts and no valid configuration is found, it automatically launches a web-based setup wizard and opens your browser to complete the setup.

The wizard will:

1. Walk you through all required configuration options
2. Validate your settings in real-time
3. Create your first admin user
4. Save the configuration and restart into normal operation

Simply run the DMRHub binary and follow the prompts in your browser. No manual file editing is needed for most deployments.

## Configuration Sources

DMRHub loads configuration from multiple sources, with later sources overriding earlier ones:

1. **Defaults** — sensible defaults are built in for most settings
2. **YAML config file** — searched in the following locations (first found wins):
   - `config.yaml` or `config.yml` in the current working directory
   - `$XDG_CONFIG_HOME/DMRHub/config.yaml` or `config.yml` (typically `~/.config/DMRHub/config.yaml` on Linux)
3. **Environment variables** — prefixed and separated by `_` (e.g., `HTTP_PORT=8080`)
4. **Command-line flags** — highest priority

## YAML Configuration Reference

Below is the full configuration structure with defaults and descriptions.

```yaml
# Logging level: debug, info, warn, or error
log-level: info

# Name of the DMR network, shown in the web interface
network-name: DMRHub

# Secret key for signing and encryption of user sessions (required)
secret: ""

# Salt used for hashing user passwords (required)
password-salt: ""

# API key for Have I Been Pwned password checking (optional)
hibp-api-key: ""

# HTTP server configuration
http:
  bind: "[::]"           # Listen address
  port: 3005             # Listen port
  canonical-host: ""     # Canonical host for generating absolute URLs (required)
  trusted-proxies: []    # List of trusted proxy IPs
  robots-txt:
    mode: disabled       # allow, disabled, or custom
    content: ""          # Custom robots.txt content (when mode is custom)
  cors:
    enabled: false
    extra-hosts: []      # Allowed CORS origins

# Database configuration
database:
  driver: sqlite         # sqlite, postgres, or mysql
  database: DMRHub.db    # Database name or file path
  host: ""               # Database host (postgres/mysql only)
  port: 0                # Database port (postgres/mysql only)
  username: ""           # Database username (postgres/mysql only)
  password: ""           # Database password (postgres/mysql only)
  extra-parameters:      # Additional connection parameters
    - "_pragma=foreign_keys(1)"
    - "_pragma=journal_mode(WAL)"

# Redis configuration (optional)
redis:
  enabled: false
  host: ""
  port: 6379
  password: ""

# DMR server configuration
dmr:
  mmdvm:
    bind: "[::]"         # MMDVM server listen address
    port: 62031          # MMDVM server port
  openbridge:
    enabled: false       # Enable experimental OpenBridge support
    bind: "[::]"
    port: 62035
  # IPSC server configuration for Motorola DMR repeaters
  ipsc:
    enabled: false         # Enable IPSC server for Motorola repeater connections
    ip: "[::]"             # IPSC server listen address
    port: 50000            # IPSC server port
    network-id: 0          # DMR peer ID for this IPSC master (required when enabled)
    auth:
      enabled: false       # Enable HMAC-SHA1 authentication on IPSC packets
  disable-radio-id-validation: false
  radio-id-url: "https://www.radioid.net/static/users.json"
  repeater-id-url: "https://www.radioid.net/static/rptrs.json"

# SMTP configuration for sending emails (optional)
smtp:
  enabled: false
  host: ""
  port: 25
  tls: none              # none, starttls, or implicit
  username: ""
  password: ""
  from: ""
  auth-method: none      # none, plain, or login

# Metrics / telemetry (optional)
metrics:
  enabled: false
  bind: "[::]"
  port: 9000
  trusted-proxies: []
  otlp-endpoint: ""      # OTLP endpoint for OpenTelemetry tracing

# PProf profiling (optional)
pprof:
  enabled: false
  bind: "[::]"
  port: 6060
  trusted-proxies: []
```

### Required Settings

At minimum, the following must be configured (the setup wizard handles these for you):

|        Setting        |                                             Description                                             |
| --------------------- | --------------------------------------------------------------------------------------------------- |
| `secret`              | Random string used for session signing/encryption. Use a 15+ character random value.                |
| `password-salt`       | Random string used for password hashing. Use a 15+ character random value, different from `secret`. |
| `http.canonical-host` | The URL your DMRHub instance is accessed at (e.g., `https://dmrhub.example.com`).                   |

### Database Configuration Examples

#### SQLite (Default)

No additional configuration needed. The database file defaults to `DMRHub.db` in the working directory.

#### PostgreSQL

```yaml
database:
  driver: postgres
  host: localhost
  port: 5432
  username: dmrhub
  password: your-password
  database: dmrhub
  extra-parameters: []
```

To set up the PostgreSQL database:

```sql
CREATE USER dmrhub WITH ENCRYPTED PASSWORD 'your-password';
CREATE DATABASE dmrhub;
ALTER DATABASE dmrhub OWNER TO dmrhub;
GRANT ALL PRIVILEGES ON DATABASE dmrhub TO dmrhub;
\c dmrhub
GRANT ALL ON SCHEMA public TO dmrhub;
```

#### MySQL

```yaml
database:
  driver: mysql
  host: localhost
  port: 3306
  username: dmrhub
  password: your-password
  database: dmrhub
  extra-parameters: []
```

## Environment Variables

Configuration can also be provided via environment variables. The variable names are derived from the YAML path by uppercasing and joining with `_`. For example:

|        YAML Path        |  Environment Variable   |
| ----------------------- | ----------------------- |
| `log-level`             | `LOG_LEVEL`             |
| `http.port`             | `HTTP_PORT`             |
| `database.driver`       | `DATABASE_DRIVER`       |
| `database.host`         | `DATABASE_HOST`         |
| `redis.enabled`         | `REDIS_ENABLED`         |
| `dmr.mmdvm.port`        | `DMR_MMDVM_PORT`        |
| `dmr.ipsc.enabled`      | `DMR_IPSC_ENABLED`      |
| `dmr.ipsc.port`         | `DMR_IPSC_PORT`         |
| `dmr.ipsc.network-id`   | `DMR_IPSC_NETWORK_ID`   |
| `smtp.enabled`          | `SMTP_ENABLED`          |
| `secret`                | `SECRET`                |
| `password-salt`         | `PASSWORD_SALT`         |
| `http.canonical-host`   | `HTTP_CANONICAL_HOST`   |
| `network-name`          | `NETWORK_NAME`          |
| `hibp-api-key`          | `HIBP_API_KEY`          |
| `metrics.otlp-endpoint` | `METRICS_OTLP_ENDPOINT` |
