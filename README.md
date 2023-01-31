# DMRHub

Run a DMR network server with a single binary. Includes private and group calls and a Parrot. Perfect for quick deployments in emergency situations. Intended for use with AREDN. 9990 Parrot and 4000 Unlink are implemented

## Current Status

This project is deployed in the Oklahoma AREDN mesh and nearing version 1.0.0. Future revisions will focus on features and testing.

## External requirements

Redis and PostgreSQL are required.

## Screenshots

![Lastheard](/doc/Screenshots/lastheard.png)

<details>
  <summary>More? Click to expand</summary>

![Repeaters](doc/Screenshots/repeaters.png)

![Easy Repeater Enrollment](doc/Screenshots/repeaters-easy.png)

![Repeater Management](doc/Screenshots/repeaters-edit.png)

![Talkgroup List](doc/Screenshots/talkgroups.png)

![Talkgroup Ownership](doc/Screenshots/talkgroup-ownership.png)

![User Approval](doc/Screenshots/user-approval.png)
</details>

## Deploying

### With Docker

`ghcr.io/usa-reddragon/DMRHub`

### Installations without Docker

Installations without Docker are not officially supported, but should still work regardless.

As an example, `hack/DMRHub.service` is a SystemD unit file to run this project as a SystemD service.

For appropriate security, you'll want to create a user, group, and directory dedicated to this:

```bash
sudo mkdir /etc/dmrhub
sudo groupadd --system dmrhub
sudo useradd --home-dir /etc/dmrhub --no-create-home --no-user-group --system --shell /sbin/nologin dmrhub
sudo chown dmrhub:dmrhub /etc/dmrhub
sudo chmod 770 /etc/dmrhub
```

Then, place your configuration environment variables into `/etc/dmrhub/env`

```bash
cat <<EOF | sudo tee /etc/dmrhub/env
LISTEN_ADDR=0.0.0.0
...
EOF

sudo chown dmrhub:dmrhub /etc/dmrhub/env
sudo chmod 660 /etc/dmrhub/env
```

Place the `DMRHub` binary into `/usr/local/bin`

```bash
sudo mv bin/DMRHub /usr/local/bin/
sudo chmod a+x /usr/local/bin/DMRHub
```

Finally, copy the SystemD unit file into `/etc/systemd/system/` and activate it

```bash
sudo cp hack/DMRHub.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now DMRHub.service
```

Logs can be viewed with `journalctl -f -u DMRHub.service`

### Setting up a database

TODO: Add instructions and provide a production-ready Docker-Compose file.

Once a database is made available, run the following commands in `psql` to create a database and user for this service.

```sql
CREATE USER dmr WITH ENCRYPTED PASSWORD 'changeme';
ALTER USER dmr WITH PASSWORD 'new_password';
CREATE DATABASE "dmr";
ALTER DATABASE dmr OWNER TO dmr;
GRANT ALL PRIVILEGES ON DATABASE dmr TO dmr;
\c dmr
GRANT ALL ON schema public TO dmr;
```

## Environment Variables

|            Name            |                          Default                          |                                                                                                              Description                                                                                                               |
| -------------------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `LISTEN_ADDR`              | `0.0.0.0`                                                 | The address to listen on. `0.0.0.0` means all IPv4 addresses                                                                                                                                                                           |
| `DMR_PORT`                 | `62031`                                                   | The port to run the DMR server on                                                                                                                                                                                                      |
| `HTTP_PORT`                | `3005`                                                    | The port to run the HTTP API, Websocket, and frontend from                                                                                                                                                                             |
| `REDIS_HOST`               | `localhost:6379`                                          | The Redis connection URL. Include the port                                                                                                                                                                                             |
| `REDIS_PASSWORD`           | `password`                                                | The password to authenticate to Redis with                                                                                                                                                                                             |
| `PG_HOST`                  | `localhost`                                               | The PostgreSQL server hostname                                                                                                                                                                                                         |
| `PG_PORT`                  | `5432`                                                    | The PostgreSQL server port                                                                                                                                                                                                             |
| `PG_USER`                  | `postgres`                                                | The PostgreSQL user to authenticate as                                                                                                                                                                                                 |
| `PG_PASSWORD`              | `password`                                                | The password of the PostgreSQL user                                                                                                                                                                                                    |
| `PG_DATABASE`              | `postgres`                                                | The PostgreSQL database to use                                                                                                                                                                                                         |
| `SECRET`                   | `secret`                                                  | The session encryption secret. Recommended to generate a 15+ character random password different from the `PASSWORD_SALT`.                                                                                                             |
| `PASSWORD_SALT`            | `salt`                                                    | The password [salt](https://en.wikipedia.org/wiki/Salt_(cryptography)). Recommended to generate a 15+ character random password different from the `SECRET`.                                                                           |
| `CORS_HOSTS`               | `http://localhost:$HTTP_PORT,http://127.0.0.1:$HTTP_PORT` | Set to a comma-separated list of URLs you expect the app to be accessed at, including the protocol and the port if not 80/443. Used for [CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS) and websocket Origin validation |
| `HIBP_API_KEY`             | not set                                                   | Set to a [HIBP](https://haveibeenpwned.com) API key to keep bad passwords away                                                                                                                                                         |
| `OTLP_ENDPOINT`            | not set                                                   | Set to an OTLP (OpenTracing) endpoint for instrumentation and tracing                                                                                                                                                                  |
| `INIT_ADMIN_USER_PASSWORD` | not set                                                   | If set on the first startup, this seeds the database with the password for the initial Admin user                                                                                                                                      |
| `TRUSTED_PROXIES`          | not set                                                   | Set to a comma-separated list of proxy IPs that are allowed to carry our traffic                                                                                                                                                       |
| `DEBUG`                    | not set                                                   | Set to any value to enable verbose logging and debug mode                                                                                                                                                                              |

## TODOs

### Soon

- details page for talkgroup with lastheard
- details page for repeater with lastheard
- details page for user with lastheard and repeaters
- users should be able to edit their name and callsign
- Fix MSTCL on master shutdown (signal trap)
- Hoseline equivalent

### Long Term

- Implement API tests
- Implement UDP server tests

## To test

- DMR sms and data packets

## Feature ideas

- Setting to use any free slot if possible. (i.e. timeslot routing where both slots are maximally active)
- Admin panels
  - see users where callsign and DMR ID don't match (due to dmr id db drift)
- server allowlist
- server blocklist
- ability to lock down traffic on one timeslot to a list of designated users. Intended for ensuring a timeslot remains open for emergency use)
- channel allowlist (maybe useful?)
- channel blocklist (this seems rife for abuse in some communities. maybe make this configurable by server admin?)
- add the ability for a talkgroup owner to create nets
- add the ability for talkgroup owner or net control operator to start/stop a net check-in
- add the ability for talkgroup owner or net control operator to see and export a check-in list (just query calls DB for TG=tg_id during net check-in period)
- distributed database? Maybe OLSR can help with the "where do I point my pi-star" problem that isn't a SPOF?
