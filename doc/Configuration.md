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
   - `--config` (`-c`) loads the given file instead of searching, and fails if it doesn't exist
3. **Environment variables** — prefixed and separated by `_` (e.g., `HTTP_PORT=8080`)
4. **Command-line flags** — highest priority

## Configuration Reference

Every option is listed below with its YAML key, default, environment variable, and command-line flag. A commented sample config with every option and its default is in [config.example.yaml](../config.example.yaml).

<!-- configulator:begin -->

| Key                               | Type           | Default                                             | Environment                       | Flag                                | Description                                                                                                                                 |
|-----------------------------------|----------------|-----------------------------------------------------|-----------------------------------|-------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `log-level`                       | string         | `info`                                              | `LOG_LEVEL`                       | `--log-level`                       | Logging level for the application. One of debug, info, warn, or error                                                                       |
| `redis.enabled`                   | boolean        | `false`                                             | `REDIS_ENABLED`                   | `--redis.enabled`                   | Enable Redis support                                                                                                                        |
| `redis.host`                      | string         |                                                     | `REDIS_HOST`                      | `--redis.host`                      | Redis host address                                                                                                                          |
| `redis.port`                      | integer        | `6379`                                              | `REDIS_PORT`                      | `--redis.port`                      | Redis port                                                                                                                                  |
| `redis.password`                  | string         |                                                     | `REDIS_PASSWORD`                  | `--redis.password`                  | Redis password                                                                                                                              |
| `database.driver`                 | string         | `sqlite`                                            | `DATABASE_DRIVER`                 | `--database.driver`                 | Database driver to use. One of sqlite, postgres, or mysql                                                                                   |
| `database.database`               | string         | `DMRHub.db`                                         | `DATABASE_DATABASE`               | `--database.database`               | Database name or path                                                                                                                       |
| `database.host`                   | string         |                                                     | `DATABASE_HOST`                   | `--database.host`                   | Database host address (postgres and mysql only)                                                                                             |
| `database.port`                   | integer        |                                                     | `DATABASE_PORT`                   | `--database.port`                   | Database port (postgres and mysql only)                                                                                                     |
| `database.username`               | string         |                                                     | `DATABASE_USERNAME`               | `--database.username`               | Database username (postgres and mysql only)                                                                                                 |
| `database.password`               | string         |                                                     | `DATABASE_PASSWORD`               | `--database.password`               | Database password (postgres and mysql only)                                                                                                 |
| `database.extra-parameters`       | list of string | `_pragma=foreign_keys(1),_pragma=journal_mode(WAL)` | `DATABASE_EXTRA_PARAMETERS`       | `--database.extra-parameters`       | Additional parameters for the database connection, e.g., sslmode=disable                                                                    |
| `secret`                          | string         |                                                     | `SECRET`                          | `--secret`                          | Secret key for the application, used for signing and encryption of the user session. Required; use a random value of at least 15 characters |
| `password-salt`                   | string         |                                                     | `PASSWORD_SALT`                   | `--password-salt`                   | Salt used for hashing user passwords. Required; use a random value of at least 15 characters, different from secret                         |
| `http.bind`                       | string         | `[::]`                                              | `HTTP_BIND`                       | `--http.bind`                       | HTTP server listen address                                                                                                                  |
| `http.port`                       | integer        | `3005`                                              | `HTTP_PORT`                       | `--http.port`                       | HTTP server port                                                                                                                            |
| `http.robots-txt.mode`            | string         | `disabled`                                          | `HTTP_ROBOTS_TXT_MODE`            | `--http.robots-txt.mode`            | Mode for serving robots.txt. One of allow, disabled, or custom                                                                              |
| `http.robots-txt.content`         | string         |                                                     | `HTTP_ROBOTS_TXT_CONTENT`         | `--http.robots-txt.content`         | Content of the robots.txt file when mode is custom                                                                                          |
| `http.cors.enabled`               | boolean        | `false`                                             | `HTTP_CORS_ENABLED`               | `--http.cors.enabled`               | Enable CORS support for the HTTP server                                                                                                     |
| `http.cors.extra-hosts`           | list of string |                                                     | `HTTP_CORS_EXTRA_HOSTS`           | `--http.cors.extra-hosts`           | List of additional allowed CORS origins                                                                                                     |
| `http.trusted-proxies`            | list of string |                                                     | `HTTP_TRUSTED_PROXIES`            | `--http.trusted-proxies`            | List of trusted proxy IPs for the HTTP server                                                                                               |
| `http.canonical-host`             | string         |                                                     | `HTTP_CANONICAL_HOST`             | `--http.canonical-host`             | URL the HTTP server is reached at, used for generating absolute URLs, e.g. https://dmrhub.example.com. Required                             |
| `dmr.mmdvm.bind`                  | string         | `[::]`                                              | `DMR_MMDVM_BIND`                  | `--dmr.mmdvm.bind`                  | MMDVM server listen address                                                                                                                 |
| `dmr.mmdvm.port`                  | integer        | `62031`                                             | `DMR_MMDVM_PORT`                  | `--dmr.mmdvm.port`                  | MMDVM server port                                                                                                                           |
| `dmr.openbridge.enabled`          | boolean        | `false`                                             | `DMR_OPENBRIDGE_ENABLED`          | `--dmr.openbridge.enabled`          | Enable experimental OpenBridge server support                                                                                               |
| `dmr.openbridge.bind`             | string         | `[::]`                                              | `DMR_OPENBRIDGE_BIND`             | `--dmr.openbridge.bind`             | OpenBridge server listen address                                                                                                            |
| `dmr.openbridge.port`             | integer        | `62035`                                             | `DMR_OPENBRIDGE_PORT`             | `--dmr.openbridge.port`             | OpenBridge server port                                                                                                                      |
| `dmr.ipsc.enabled`                | boolean        | `false`                                             | `DMR_IPSC_ENABLED`                | `--dmr.ipsc.enabled`                | Enable IPSC server support                                                                                                                  |
| `dmr.ipsc.bind`                   | string         | `[::]`                                              | `DMR_IPSC_BIND`                   | `--dmr.ipsc.bind`                   | IPSC server listen address                                                                                                                  |
| `dmr.ipsc.port`                   | integer        | `50000`                                             | `DMR_IPSC_PORT`                   | `--dmr.ipsc.port`                   | IPSC server port                                                                                                                            |
| `dmr.ipsc.network-id`             | integer        |                                                     | `DMR_IPSC_NETWORK_ID`             | `--dmr.ipsc.network-id`             | DMR network ID that identifies this server to IPSC peers. Required when IPSC is enabled                                                     |
| `dmr.disable-radio-id-validation` | boolean        | `false`                                             | `DMR_DISABLE_RADIO_ID_VALIDATION` | `--dmr.disable-radio-id-validation` | Disable validation of radio IDs in DMR packets, allowing any 7- to 9-digit number to be used as a radio ID                                  |
| `dmr.radio-id-url`                | string         | `https://www.radioid.net/static/users.json`         | `DMR_RADIO_ID_URL`                | `--dmr.radio-id-url`                | URL to fetch radio ID information for validation and display purposes. Expected JSON format is the same as RadioID.net.                     |
| `dmr.repeater-id-url`             | string         | `https://www.radioid.net/static/rptrs.json`         | `DMR_REPEATER_ID_URL`             | `--dmr.repeater-id-url`             | URL to fetch repeater information for validation and display purposes. Expected JSON format is the same as RadioID.net.                     |
| `smtp.enabled`                    | boolean        | `false`                                             | `SMTP_ENABLED`                    | `--smtp.enabled`                    | Enable SMTP support for sending emails                                                                                                      |
| `smtp.host`                       | string         |                                                     | `SMTP_HOST`                       | `--smtp.host`                       | SMTP server host address                                                                                                                    |
| `smtp.port`                       | integer        | `25`                                                | `SMTP_PORT`                       | `--smtp.port`                       | SMTP server port                                                                                                                            |
| `smtp.tls`                        | string         | `none`                                              | `SMTP_TLS`                        | `--smtp.tls`                        | SMTP TLS mode. One of none, starttls, or implicit                                                                                           |
| `smtp.username`                   | string         |                                                     | `SMTP_USERNAME`                   | `--smtp.username`                   | SMTP server username                                                                                                                        |
| `smtp.password`                   | string         |                                                     | `SMTP_PASSWORD`                   | `--smtp.password`                   | SMTP server password                                                                                                                        |
| `smtp.from`                       | string         |                                                     | `SMTP_FROM`                       | `--smtp.from`                       | Email address to use as the sender                                                                                                          |
| `smtp.auth-method`                | string         | `none`                                              | `SMTP_AUTH_METHOD`                | `--smtp.auth-method`                | SMTP authentication method. One of none, plain, or login                                                                                    |
| `network-name`                    | string         | `DMRHub`                                            | `NETWORK_NAME`                    | `--network-name`                    | Name of the DMR network, shown in the web interface                                                                                         |
| `metrics.enabled`                 | boolean        | `false`                                             | `METRICS_ENABLED`                 | `--metrics.enabled`                 | Enable metrics collection and export                                                                                                        |
| `metrics.bind`                    | string         | `[::]`                                              | `METRICS_BIND`                    | `--metrics.bind`                    | Metrics server listen address                                                                                                               |
| `metrics.port`                    | integer        | `9000`                                              | `METRICS_PORT`                    | `--metrics.port`                    | Metrics server port                                                                                                                         |
| `metrics.trusted-proxies`         | list of string |                                                     | `METRICS_TRUSTED_PROXIES`         | `--metrics.trusted-proxies`         | List of trusted proxy IPs for the metrics server                                                                                            |
| `metrics.otlp-endpoint`           | string         |                                                     | `METRICS_OTLP_ENDPOINT`           | `--metrics.otlp-endpoint`           | OTLP endpoint for exporting OpenTelemetry tracing data                                                                                      |
| `pprof.enabled`                   | boolean        | `false`                                             | `PPROF_ENABLED`                   | `--pprof.enabled`                   | Enable PProf profiling and debugging support                                                                                                |
| `pprof.bind`                      | string         | `[::]`                                              | `PPROF_BIND`                      | `--pprof.bind`                      | PProf server listen address                                                                                                                 |
| `pprof.trusted-proxies`           | list of string |                                                     | `PPROF_TRUSTED_PROXIES`           | `--pprof.trusted-proxies`           | List of trusted proxy IPs for the PProf server                                                                                              |
| `pprof.port`                      | integer        | `6060`                                              | `PPROF_PORT`                      | `--pprof.port`                      | PProf server port                                                                                                                           |
| `hibp-api-key`                    | string         |                                                     | `HIBP_API_KEY`                    | `--hibp-api-key`                    | API key for the Have I Been Pwned service, used for checking if passwords have been compromised                                             |

<!-- configulator:end -->

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

Configuration can also be provided via environment variables. The variable names are derived from the YAML path by uppercasing, replacing `-` with `_`, and joining nested keys with `_` (for example, `http.canonical-host` becomes `HTTP_CANONICAL_HOST`). List values are comma-separated. The Environment column of the [Configuration Reference](#configuration-reference) lists every variable.
