# Plugins

A plugin is a program that answers the calls to the DMR IDs an admin gives it. The parrot and the location server are plugins. A plugin runs in one of two ways:

- **Inside DMRHub.** DMRHub runs the plugin itself, as a WebAssembly module, with nothing else to install. The parrot and the location server come built in, and admins upload others as `.wasm` files. This is on by default.
- **As its own program.** The plugin runs next to DMRHub or on another machine, and connects to the hub over TLS.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Settings

Go to **Admin**, then **Setup**, and find the **Plugins** card in **DMR Settings**.

| Setting                                        | What it is                                                                   | Configuration key        |
| ---------------------------------------------- | ---------------------------------------------------------------------------- | ------------------------ |
| Run plugins inside DMRHub                      | Whether DMRHub runs the built-in and uploaded plugins. On by default.        | `dmr.plugins.in-process` |
| Accept plugins that run as their own programs  | Whether DMRHub listens for plugins. Off by default.                          | `dmr.plugins.enabled`    |
| Port                                           | The TCP port that plugins connect to. 3006 by default.                       | `dmr.plugins.port`       |
| Certificate file                               | A TLS certificate. DMRHub reads it again when it changes.                    | `dmr.plugins.cert-file`  |
| Key file                                       | The key of the certificate.                                                  | `dmr.plugins.key-file`   |

Turn off **Run plugins inside DMRHub** only when you run every plugin as its own program. A Setup save turns it on or off at once on the replica that took the save; the other replicas follow when they restart, as does a hub that started with both settings off. The listener binds to all addresses by default (`dmr.plugins.bind`), and starts when DMRHub restarts. With no certificate file, DMRHub makes a certificate of its own from its secret, the same on every replica. Plugins then check it by its fingerprint, which the Plugins page shows.

## Plugins inside DMRHub

When plugins run inside DMRHub, DMRHub adds the parrot with the ID 9990 and the location server with the IDs 5050 to 5059. A built-in plugin whose IDs another plugin already has starts with no IDs. The Plugins page (`<siteurl>/admin/plugins`) lists every plugin, where it runs (**Built in**, **Uploaded** or **External**), its version, its IDs and its status.

### Upload a plugin

1. Go to `<siteurl>/admin/plugins` and click **Upload a plugin**.
2. Choose the plugin's `.wasm` file. DMRHub shows its name, version, author and what it does, the IDs it asks for, and the hosts it asks to reach.
3. Change the IDs if you want, and allow the hosts that it may reach.
4. Click **Upload and start**.

A plugin file may be at most 32 MiB.

A plugin's page turns it on and off, changes its IDs and the hosts it may reach, and takes a newer `.wasm` file of an uploaded plugin. A new version takes over when the plugin's calls end, after 5 quiet seconds or 5 minutes at most. The hosts that a new version adds start out not allowed. A built-in plugin cannot be removed; turn it off instead.

### The sandbox

A plugin inside DMRHub:

- reaches no files and no environment variables;
- reaches only the hosts that an admin allowed on its page, as `host:port`. A host allowed by name must have a public address, so that its DNS cannot point the plugin at the hub's own network. An IP address that an admin allowed may be private, such as a device on your LAN. Loopback and link-local addresses are never reached;
- has 64 MiB of memory, and half a CPU core over each 10 seconds, after it starts;
- has at most 4 connections at once;
- must say who it is within 10 seconds of its start.

A plugin that goes over a limit, or ends, is stopped, and starts again after a wait that doubles from a second to a minute. Its log lines go to DMRHub's log, at most 20 a second.

DMRHub compiles the plugins on amd64 and arm64. On other machines, such as a 32-bit Raspberry Pi, wazero's interpreter runs them, which is much slower.

### Several replicas

Only one replica runs each plugin inside DMRHub: the one that holds the plugin's lease in the KV store (Redis). When that replica stops, another takes the plugin over within about 20 seconds, or within 5 seconds when the replica stops cleanly. Uploaded plugins are kept in the database, so every replica has them.

## Plugins that run as their own programs

1. Turn on **Accept plugins that run as their own programs**, and restart DMRHub.
2. Go to `<siteurl>/admin/plugins` and click **Add an external plugin**.
3. Give it a name and its IDs, as one or more ranges, then click **Save**.
4. Copy the token. DMRHub shows it only once.

The page shows the address and the certificate fingerprint that the plugin connects with. **Make a new token** replaces the token, and disconnects the plugin until it uses the new one. Turning a plugin off or deleting it disconnects it, and its IDs are free again.

No two plugins share an ID. DMRHub gives no IDs to an external plugin on its own: the admin picks them all.

## What a plugin gets and sends

- A private call to one of a plugin's IDs goes only to the plugin.
- A group call to one of a plugin's IDs goes to the plugin. When a talkgroup has that ID, its subscribers get the call too.
- A plugin sends calls only from its own IDs. It sends private calls to radios, and group calls only to talkgroups among its IDs. It never sends to another plugin's IDs.
- The hub routes a plugin's calls as it routes the calls of a repeater, with the same limit of one call at a time on each timeslot. A group call of a plugin goes to the OpenBridge peers whose rules take it. A private call of a plugin goes to the peers only when the radio is not on this hub, so that a reply to a radio here does not reach it twice.
- A plugin can ask for the radios whose owners turned position tracking on, and where they are heard. It can send the positions those radios report to its IDs. The hub keeps a position only when the radio's owner turned tracking on, and then shows it on the map and sends it to APRS-IS as the owner chose.

When DMRHub runs as several replicas, a plugin that runs as its own program connects to any one of them. A newer connection of the same plugin ends the older one.

When a replica stops and other replicas run, as in a rolling update, the replica first drains its plugins. It tells each plugin that runs as its own program and is connected to it that it stops, and from then on refuses new plugin connections. A plugin built with the plugin SDK of this version then opens a second connection, which reaches another replica, while it keeps the first one. When the new connection is up, the plugin sends on it, and the old replica ends the old connection about a second later. A burst that comes on both connections reaches the plugin once. The plugin sees no disconnection, and no call to its IDs is lost. The replica waits for this as long as it drains its repeaters (`dmr.drain-timeout`, 25 seconds by default). A plugin built with an older SDK does not know this message. It connects again as before when its connection ends, and the calls to its IDs in that gap are lost.

## The plugins that come with DMRHub as programs

The parrot and the location server also come as programs, for a hub that does not run plugins inside DMRHub. The release archives and the Docker image include them. Each takes the hub's address and the fingerprint as flags or environment variables, and its token from the environment:

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

Before plugins, DMRHub answered 9990 and 5050 to 5059 itself. With plugins inside DMRHub on, as by default, the parrot and the location server answer those IDs again. To run them as programs instead:

1. Turn off **Run plugins inside DMRHub**, and turn on **Accept plugins that run as their own programs**.
2. Add an external plugin named Parrot with the ID 9990, and run `dmrhub-parrot` with its token.
3. Add an external plugin named Location with the IDs 5050 to 5059, and run `dmrhub-locator` with its token.

The upgrade removes the user and the talkgroup 9990 that DMRHub made for the built-in parrot, unless an admin renamed them. Calls to and from the old parrot stay in the last heard list.

## Write a plugin

A plugin in Go uses the SDK in `github.com/USA-RedDragon/DMRHub/sdk/plugin`:

```go
package main

import (
	"context"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

func main() {
	plugin.MainWith(&pluginv1.Hello{
		Name:        "Net check-in",
		Version:     "0.4.1",
		Author:      "N1CALL",
		Description: "Logs each radio that calls 9100 during a net and answers with a short tone.",
		Ids:         []*pluginv1.IDRange{{First: 9100, Last: 9100}},
		Hosts:       []*pluginv1.Host{{Address: "nets.example.org:443", Reason: "Posts the check-in list when a net ends."}},
	}, func(ctx context.Context, conn *plugin.Conn) error {
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

Build it to run inside DMRHub, and upload the `.wasm` file:

```bash
GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags='-s -w' -o net-checkin.wasm .
```

The same code built for your computer runs as its own program, with the flags and environment variables of the plugins that come with DMRHub. `conn.Dial` opens a TCP connection to a host; inside DMRHub, only to the hosts that an admin allowed. Use it as the `DialContext` of an `http.Transport` to reach a web API. A plugin inside DMRHub has no certificate authorities of its own, so a TLS client in it needs some, such as `golang.org/x/crypto/x509roots/fallback`.

`plugin.DataOf` and `plugin.BurstOf` turn the bursts into the DMRD packets of [dmrgo](https://github.com/USA-RedDragon/dmrgo), whose voice and data receivers and transmitters decode and build calls. The parrot in `plugins/parrot` and the location server in `plugins/locator` are examples.

A plugin in another language uses the messages in `proto/dmrhub/plugin/v1/plugin.proto`.

- **As its own program,** it opens one `Connect` stream of the gRPC service over TLS, with its token in the `authorization` metadata as `Bearer <token>`.
- **Inside DMRHub,** it is a WASI preview 1 (`wasip1`) command. It reads the `ConnectResponse` messages on its stdin and writes the `ConnectRequest` messages on its stdout. Each message is its length, 4 bytes big-endian, then its protobuf encoding. Its first message is a `Hello`, and the hub answers with the `Welcome`. It reaches the network with `NetRequest` messages, which the hub answers with `NetResponse` messages.
