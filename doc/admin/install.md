# Install DMRHub

This page lists the ways to install and run DMRHub, and the ports that it uses. For a ham who wants to use a DMRHub network, see [Get started](../getting-started.md).

## What you need

DMRHub is one program. By default it keeps its data in a SQLite file, so it needs no other services. For more than one server, or for more load, it can also use:

- **PostgreSQL**, **MySQL** or **MariaDB** as the database.
- **Redis**, so that several DMRHub replicas share the work. See [Replicas and scaling](replicas.md).

See [Configuration](configuration.md) for the database settings.

## First start and the setup wizard

DMRHub starts the setup wizard instead of the network when its configuration is not valid, when no admin user exists yet, or when it cannot reach its database.

**Finish setup before you expose a new DMRHub to the internet.** The setup wizard has no password. Anyone who opens it can change the settings, and the first visitor to make an account becomes the super admin. Keep port 3005 closed to the internet, or open it only to you, until you have made the admin.

1. DMRHub writes the reason and the wizard address to its log, for example:

   ```text
   INF No admin exists yet
   INF Opening setup wizard at http://localhost:3005/setup
   ```

   The wizard listens on `http.bind` and `http.port`, the same address as the web interface. DMRHub also tries to open a web browser on the server. On a server without a desktop, or in a container, this fails with a `Failed to open browser` error in the log. That error is normal.

2. Open `http://<server>:<http.port>/setup` in a browser, such as `http://<server>:3005/setup`. You do not need anything from the log.
3. Fill in the settings and click **Save**. DMRHub checks them and saves them in the database (see [Setup settings](configuration.md#setup-settings)). When no deployment sets `secret` and `password-salt`, DMRHub makes them at the first start and keeps them in the database. When the deployment sets every needed setting, the wizard opens on the next page.
4. Make the first admin user on the **Create the first admin** page. DMRHub checks the DMR ID and callsign against RadioID.net when those checks are on (see [RadioID checks](users.md#radioid-checks)). This user is the super admin (see [Admins and the super admin](overview.md#admins-and-the-super-admin)).
5. DMRHub stops the wizard and starts the network in the same process, with the new settings. Log in at `<siteurl>/login`.

The wizard is the only way to make the first admin user. It starts while no admin exists, also when the configuration is complete. The database lets only one visitor make the first admin, also when several replicas run the wizard at once. A visitor who comes second gets the message that the admin exists, and can log in when that admin gives them an account.

The wizard changes settings only on a new DMRHub. When an admin exists, it shows `DMRHub has an admin, so the setup wizard cannot change the settings.` When DMRHub cannot reach a database setting that the wizard saved before in `dmrhub-database.json`, or a SQLite file that has a user, it shows `A database setting was saved before, so the setup wizard cannot change it.` In both cases, fix the setting in the config file, the environment or the flags, then restart DMRHub. A database setting there wins over `dmrhub-database.json`.

![The setup wizard with the General Settings card at the top](../Screenshots/setup-wizard-general.png)

![The Create the first admin page, filled in with the DMR ID 3000001 and the callsign N0CALL](../Screenshots/setup-admin-user.png)

## Docker

DMRHub publishes images for `linux/amd64`, `linux/arm64`, `linux/arm/v7` and `linux/arm/v6`, at:

- `ghcr.io/usa-reddragon/dmrhub`
- `docker.io/jamcswain/dmrhub`

Each release gets four tags. For release v1.3.16, they are `v1.3.16`, `v1.3`, `v1` and `latest`. Use `v1` to get each new v1 release when you pull. Do not use the tags without the `v`, such as `1`: they are old images from 2023 and do not get updates.

The image runs `/app/DMRHub` as user and group 65534, with `/app` as the working directory. The image has no shell. Give DMRHub a volume for its data, and tell it to keep its database there:

1. Make the volume and give it to user 65534. A new volume belongs to root, and DMRHub cannot write to it:

   ```bash
   docker volume create dmrhub-data
   docker run --rm -v dmrhub-data:/data alpine chown 65534:65534 /data
   ```

2. Start DMRHub:

   ```bash
   docker run -d \
       --restart unless-stopped \
       --name dmrhub \
       -e DATABASE_DATABASE=/data/DMRHub.db \
       -p 3005:3005 \
       -p 62031:62031/udp \
       -v dmrhub-data:/data \
       ghcr.io/usa-reddragon/dmrhub:v1
   ```

   `DATABASE_DATABASE` puts the SQLite file in the volume. DMRHub keeps the Setup settings, the secret and the password salt in that database. Without this setting, DMRHub keeps its database in the container, where you lose it when you remove the container.

3. Before you expose port 3005 to the internet, open `http://<server>:3005/` and finish the setup wizard (see [First start and the setup wizard](#first-start-and-the-setup-wizard)).

Add a `-p` option for each other port that you turn on (see [Ports](#ports)). To use another port on the host, change the number before the `:`.

### Settings from environment variables

You can give each setting as an environment variable. See [Configuration](configuration.md#environment-variables) for the names. For example:

```bash
docker run -d \
    --restart unless-stopped \
    --name dmrhub \
    -e SECRET=<random value> \
    -e PASSWORD_SALT=<another random value> \
    -e HTTP_CANONICAL_HOST=https://dmrhub.example.com \
    -e DATABASE_DATABASE=/data/DMRHub.db \
    -p 3005:3005 \
    -p 62031:62031/udp \
    -v dmrhub-data:/data \
    ghcr.io/usa-reddragon/dmrhub:v1
```

A setting from the environment is locked: the Setup page shows it as set by the deployment and cannot change it. See [Setup settings](configuration.md#setup-settings).

### Settings from a file

To give DMRHub a configuration file of your own, mount it at `/app/config.yaml`, the working directory of the image:

```bash
-v /path/to/config.yaml:/app/config.yaml:ro
```

DMRHub does not read a file at `/config.yaml`. DMRHub never writes the configuration file, so it can be read-only. A setting that the file sets is locked on the Setup page.

## Binary releases

[GitHub Releases](https://github.com/USA-RedDragon/DMRHub/releases) has a `.tar.gz` file for each of these platforms:

- Linux: `amd64`, `arm64`, `armv7`, `armv6`, `riscv64`
- macOS: `amd64`, `arm64`
- Windows: `amd64`, `arm64`

Each file has the `DMRHub` program, the `dmrhub-parrot` and `dmrhub-locator` plugin programs, the license and a README. Only Linux is supported.

### Run DMRHub with systemd

1. Make a system user and its directory:

   ```bash
   sudo groupadd --system dmrhub
   sudo useradd --system --gid dmrhub --home-dir /etc/dmrhub --no-create-home \
       --shell /sbin/nologin dmrhub
   sudo mkdir /etc/dmrhub
   sudo chown dmrhub:dmrhub /etc/dmrhub
   sudo chmod 770 /etc/dmrhub
   ```

2. Install the program:

   ```bash
   tar -xzf DMRHub_<version>_linux_amd64.tar.gz DMRHub
   sudo install -m 0755 DMRHub /usr/local/bin/DMRHub
   ```

3. Install the unit file [`hack/dmrhub.service`](../../hack/dmrhub.service) from the repository, and start DMRHub:

   ```bash
   sudo cp hack/dmrhub.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now dmrhub.service
   ```

4. Before you expose port 3005 to the internet, open `http://<server>:3005/` and finish the setup wizard (see [First start and the setup wizard](#first-start-and-the-setup-wizard)).

5. To see the log as it grows:

   ```bash
   journalctl -f -u dmrhub.service
   ```

The unit runs DMRHub as the `dmrhub` user, in `/etc/dmrhub`. These files are in that directory:

| File | What it is |
| ---- | ---------- |
| `/etc/dmrhub/DMRHub.db` | The SQLite database, with `DMRHub.db-wal` and `DMRHub.db-shm` |
| `/etc/dmrhub/config.yaml` | A configuration file of your own, if you write one. DMRHub never writes it. A setting that it sets is locked on the Setup page. |
| `/etc/dmrhub/dmrhub-database.json` | The database setting that the setup wizard saves when DMRHub could not reach its database |
| `/etc/dmrhub/env` | Environment variables for DMRHub, one `NAME=value` on each line. The file is optional. |

The unit mounts `/etc` read-only for DMRHub, except `/etc/dmrhub`, and hides the home directories.

## Ports

| Port  | Protocol | What it is | When it is open |
| ----- | -------- | ---------- | --------------- |
| 3005  | TCP      | The web interface, the API and the websockets (`http.port`) | Always. The setup wizard uses it too. |
| 62031 | UDP      | The MMDVM server for hotspots and repeaters (`dmr.mmdvm.port`) | Always |
| 62032 | UDP      | WebTransport for browser hotspots and radios (`dmr.mmdvm.webtransport.port`) | When `dmr.mmdvm.webtransport.enabled` is on |
| 62035 | UDP      | The OpenBridge server (`dmr.openbridge.port`) | When `dmr.openbridge.enabled` is on |
| 50000 | UDP      | The IPSC server for Motorola repeaters (`dmr.ipsc.port`) | When `dmr.ipsc.enabled` is on |
| 3006  | TCP      | The listener for plugins that run as their own programs (`dmr.plugins.port`) | When `dmr.plugins.enabled` is on |
| 9000  | TCP      | Prometheus metrics at `/metrics` (`metrics.port`) | When `metrics.enabled` is on |
| 6060  | TCP      | PProf (`pprof.port`) | When `pprof.enabled` is on |

Behind a reverse proxy, set `http.canonical-host` to the address that users open, and `http.trusted-proxies` to the addresses of the proxy. The proxy must also pass websockets.
