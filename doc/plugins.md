# Plugins

A plugin is a program that answers the calls to the DMR IDs an admin gives it. The parrot and the location server are plugins. A plugin runs on its own, next to DMRHub or on another machine, and connects to the hub over TLS.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Turn on the plugin listener

Go to **Admin**, then **Setup**, and find the **Plugins** card in **DMR Settings**.

| Setting          | What it is                                                                  | Configuration key       |
| ---------------- | --------------------------------------------------------------------------- | ----------------------- |
| Accept plugins   | Whether DMRHub listens for plugins. Off by default.                         | `dmr.plugins.enabled`   |
| Port             | The TCP port that plugins connect to. 3006 by default.                      | `dmr.plugins.port`      |
| Certificate file | A TLS certificate. DMRHub reads it again when it changes.                   | `dmr.plugins.cert-file` |
| Key file         | The key of the certificate.                                                 | `dmr.plugins.key-file`  |

The listener binds to all addresses by default (`dmr.plugins.bind`). With no certificate file, DMRHub makes a certificate of its own from its secret, the same on every replica. Plugins then check it by its fingerprint, which the Plugins page shows.

## Add a plugin

1. Go to `<siteurl>/admin/plugins` (**Admin**, then **Plugins**).
2. Click **Add a plugin**.
3. Give it a name and its IDs, as one or more ranges, then click **Save**.
4. Copy the token. DMRHub shows it only once.

The page shows the address and the certificate fingerprint that the plugin connects with. **Make a new token** replaces the token, and disconnects the plugin until it uses the new one. Turning a plugin off or deleting it disconnects it, and its IDs are free again.

No two plugins share an ID. DMRHub gives no IDs to a plugin on its own: the admin picks them all.

## What a plugin gets and sends

- A private call to one of a plugin's IDs goes only to the plugin.
- A group call to one of a plugin's IDs goes to the plugin. When a talkgroup has that ID, its subscribers get the call too.
- A plugin sends calls only from its own IDs. It sends private calls to radios, and group calls only to talkgroups among its IDs. It never sends to another plugin's IDs.
- The hub routes a plugin's calls as it routes the calls of a repeater, with the same limit of one call at a time on each timeslot. A group call of a plugin goes to the OpenBridge peers whose rules take it. A private call of a plugin goes to the peers only when the radio is not on this hub, so that a reply to a radio here does not reach it twice.
- A plugin can ask for the radios whose owners turned position tracking on, and where they are heard. It can send the positions those radios report to its IDs. The hub keeps a position only when the radio's owner turned tracking on, and then shows it on the map and sends it to APRS-IS as the owner chose.

When DMRHub runs as several replicas, a plugin connects to any one of them. A newer connection of the same plugin ends the older one.

## The plugins that come with DMRHub

The release archives and the Docker image include two plugins. Each takes the hub's address and the fingerprint as flags or environment variables, and its token from the environment:

| Flag           | Environment variable        | What it is                                                             |
| -------------- | --------------------------- | ---------------------------------------------------------------------- |
| `-address`     | `DMRHUB_PLUGIN_ADDRESS`     | The address and port from the Plugins page, such as `hub.example.org:3006` |
| `-fingerprint` | `DMRHUB_PLUGIN_FINGERPRINT` | The fingerprint from the Plugins page. Leave it empty when the hub has a certificate from a certificate authority. |
|                | `DMRHUB_PLUGIN_TOKEN`       | The token of the plugin                                                |

A plugin connects again by itself when it loses the hub, after a wait that grows from a second to 30 seconds.

### Parrot

`dmrhub-parrot` plays each voice call to its IDs back to the caller, three seconds after the call ends, as a private call. It answers private and group calls. Give it 9990, the ID radios usually call the parrot at.

```bash
DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... dmrhub-parrot -address hub.example.org:3006 -fingerprint 4F:2A:...
```

### Location server

`dmrhub-locator` answers the registrations (ARS) and location reports of radios, asks the radios whose owners turned tracking on for their positions, and gives the positions to the hub. Give it 5050 to 5059. For what users see, see [Positions](positions.md).

```bash
DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... dmrhub-locator -address hub.example.org:3006 -fingerprint 4F:2A:...
```

The location server keeps nothing on disk. After a restart, it learns each open request from the next report that answers it, and asks a tracked radio for its position the next time the radio is heard.

### In Docker

The plugins are in the DMRHub image, next to DMRHub:

```bash
docker run -e DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... --entrypoint /app/dmrhub-parrot ghcr.io/usa-reddragon/dmrhub \
  -address hub.example.org:3006 -fingerprint 4F:2A:...
```

## Upgrading from a hub with the built-in parrot

Before plugins, DMRHub answered 9990 and 5050 to 5059 itself. It no longer does. After the upgrade:

1. Turn on **Accept plugins**.
2. Add a plugin named Parrot with the ID 9990, and run `dmrhub-parrot` with its token.
3. Add a plugin named Location with the IDs 5050 to 5059, and run `dmrhub-locator` with its token.

The upgrade removes the user and the talkgroup 9990 that DMRHub made for the built-in parrot, unless an admin renamed them. Calls to and from the old parrot stay in the last heard list.

## Write a plugin

A plugin in Go uses the SDK in `github.com/USA-RedDragon/DMRHub/sdk/plugin`:

```go
package main

import (
	"context"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
)

func main() {
	plugin.Main(func(ctx context.Context, conn *plugin.Conn) error {
		for {
			m, err := conn.Recv()
			if err != nil {
				return err
			}
			if b := m.GetBurst(); b != nil {
				// Answer the burst with conn.Send.
			}
		}
	})
}
```

`plugin.DataOf` and `plugin.BurstOf` turn the bursts into the DMRD packets of [dmrgo](https://github.com/USA-RedDragon/dmrgo), whose voice and data receivers and transmitters decode and build calls. The parrot in `plugins/parrot` and the location server in `plugins/locator` are examples.

A plugin in another language uses the gRPC service in `proto/dmrhub/plugin/v1/plugin.proto`. It opens one `Connect` stream over TLS, with its token in the `authorization` metadata as `Bearer <token>`.
