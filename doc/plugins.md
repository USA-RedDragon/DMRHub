# Plugins

A plugin is a program that answers the calls to the DMR IDs that an admin gives it. The parrot (9990) and the location server (5050 to 5059) are plugins. A plugin runs in one of two ways:

- **Inside DMRHub.** DMRHub runs the plugin as a WebAssembly module, in a sandbox. You install nothing else. The parrot and the location server are built in, and an admin can upload other plugins as `.wasm` files. This is on by default.
- **As its own program.** The plugin runs next to DMRHub or on another computer, and connects to the hub over TLS.

This page has two parts. [Run plugins](#run-plugins) is for admins. [Write a plugin](#write-a-plugin) is for developers.

In this page, `<siteurl>` is the URL of your DMRHub instance.

## Run plugins

### Settings

Go to **Admin**, then **Setup**, and open the **Plugins** tab (`<siteurl>/admin/setup#plugins`).

<!-- screenshot: setup-plugins-card.png | The Plugins card in DMR Settings on /admin/setup, with "Run plugins inside DMRHub" on and "Accept plugins that run as their own programs" on, so that the Port, Certificate file and Key file fields show -->

| Setting                                       | What it does                                                                | Configuration key        | Default |
| --------------------------------------------- | --------------------------------------------------------------------------- | ------------------------ | ------- |
| Run plugins inside DMRHub                     | Runs the built-in plugins and the uploaded plugins.                         | `dmr.plugins.in-process` | On      |
| Accept plugins that run as their own programs | Opens the listener that external plugins connect to.                        | `dmr.plugins.enabled`    | Off     |
| Port                                          | The TCP port of the listener. It must not be the HTTP port.                 | `dmr.plugins.port`       | 3006    |
| Certificate file                              | The TLS certificate of the listener. DMRHub reads it again when it changes. | `dmr.plugins.cert-file`  | Empty   |
| Key file                                      | The key of the certificate.                                                 | `dmr.plugins.key-file`   | Empty   |

The listener binds to `dmr.plugins.bind`, which is `[::]` (all addresses) by default. DMRHub reads the listener settings at start only, so the Setup page shows them but cannot change them.

- Turn off **Run plugins inside DMRHub** only when every plugin runs as its own program. A Setup save turns it on or off at once on the replica that took the save. The other replicas follow when they restart. A hub that started with both settings off also needs a restart.
- The listener starts when DMRHub restarts.
- With no certificate file, DMRHub makes its own certificate from its secret. Every replica has the same certificate. Plugins then check it by its fingerprint, which the plugin's page shows.

### The plugins page

Go to **Admin**, then **Plugins** (`<siteurl>/admin/plugins`). The page lists each plugin with its name, where it runs, its version, its IDs, its status and its calls today.

<!-- screenshot: admin-plugins-list.png | /admin/plugins with the built-in Parrot and Location server plugins running, one uploaded plugin and one external plugin, showing the Runs and Status columns -->

The **Runs** column shows one of these:

| Runs     | Meaning                                               |
| -------- | ----------------------------------------------------- |
| Built in | The parrot or the location server, inside DMRHub.     |
| Uploaded | A `.wasm` file that an admin uploaded, inside DMRHub. |
| External | A plugin that runs as its own program.                |

The **Status** of a plugin inside DMRHub is one of these:

| Status                                             | Meaning                                                                                                                   |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Running in DMRHub                                  | The plugin runs.                                                                                                          |
| Starting                                           | The plugin starts.                                                                                                        |
| Stopped at `<time>`, starts again in `<n>` s       | The plugin crashed, ended or went over a limit. DMRHub starts it again after a wait. The plugin's page tells why it stopped. |
| Waiting for its IDs, `<id>` is used by `<plugin>`  | A plugin that is on already has one of its IDs. It starts when the IDs are free.                                          |
| Off                                                | An admin turned the plugin off.                                                                                           |

The **Status** of an external plugin is **Connected for** a time and from an address, **Not connected since** a time, **Not connected yet**, or **Off**.

### The built-in plugins

When **Run plugins inside DMRHub** is on, DMRHub adds two built-in plugins:

| Plugin          | IDs          | What it does                                                                                                                                                                     |
| --------------- | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Parrot          | 9990         | Records each voice call to its IDs, private or group, and plays it back to the caller as a private call 3 seconds after the call ends. It keeps at most 3 minutes of a call. |
| Location server | 5050 to 5059 | Answers radio registrations (ARS) and location reports, asks the radios whose owners turned tracking on for their positions, and gives the positions to the hub. See [Positions](positions.md). |

You cannot remove a built-in plugin. Turn it off on its page instead. You can change its IDs.

### Upload a plugin

1. Go to `<siteurl>/admin/plugins` and click **Upload a plugin**.
2. In **Plugin file**, choose the plugin's `.wasm` file. DMRHub starts the plugin to read its manifest. It then shows the name, version, author, description, the IDs the plugin asks for, and the hosts it asks to reach.
3. Change the IDs if necessary. Select the hosts that the plugin can reach.
4. Click **Upload and start**.

<!-- screenshot: admin-plugin-upload.png | /admin/plugins/upload after a .wasm file is chosen, showing the manifest (Name, Version, Author), the IDs field and the Network host checkboxes -->

A plugin file is at most 32 MiB. DMRHub refuses a file when:

- the name is empty, longer than 64 characters, or the name of another plugin that is not built in;
- the plugin gives no version;
- the author is longer than 64 characters, or the description is longer than 500 characters;
- its IDs are not free;
- it asks for more than 16 hosts, a host that is not `host:port`, the same host twice, or gives a reason longer than 200 characters.

To upload a newer version of an uploaded plugin, open the plugin and use **Upload a new version**. Do not upload it again with **Upload a plugin**.

### Change a plugin inside DMRHub

Open the plugin from the plugins page. Its page has:

- **On**, to turn the plugin on and off.
- **IDs**, the radio and talkgroup IDs of the plugin, as single IDs and ranges such as `9990, 5050-5059`.
- **Network**, the hosts that the plugin asks to reach. It can reach only the hosts that you select.
- **Upload a new version**, for an uploaded plugin only. The new version takes over when the plugin's calls end: after 5 quiet seconds, or after 5 minutes at most. The hosts that the new version adds start out not allowed.
- **Remove plugin**, for an uploaded plugin only. After you remove it, calls to its IDs go nowhere.

Click **Save** to keep the changes.

<!-- screenshot: admin-plugin-inside.png | /admin/plugins/<id> of an uploaded plugin, with On, IDs, Network and Upload a new version -->

### Add a plugin that runs as its own program

1. Turn on **Accept plugins that run as their own programs** in Setup, and restart DMRHub.
2. Go to `<siteurl>/admin/plugins` and click **Add an external plugin**.
3. Give it a **Name** and its **IDs**, as one or more ranges. Use **Add a range** for each more range. Click **Save**.
4. Copy the token. DMRHub shows it only once.

<!-- screenshot: admin-plugin-token.png | /admin/plugins/new after Save, showing the one-time token with the Copy button, the Address and the Certificate fingerprint -->

The **Connection** card of the plugin shows the address and the certificate fingerprint that the plugin connects with.

- **Make a new token** replaces the token. The plugin disconnects until it uses the new token.
- When you turn **Plugin on** off, or **Delete** the plugin, DMRHub disconnects it and its IDs are free again.

Rules for IDs:

- Two plugins that are on never share an ID, except a built-in plugin.
- A built-in plugin never keeps another plugin from its IDs. When another plugin that is on has some of them, the built-in plugin waits until they are free.
- A plugin that is off keeps its IDs, but another plugin can take them. The first plugin then cannot be turned on until its IDs are free.
- A built-in plugin and another plugin can have the same name. Two plugins that are not built in cannot.
- DMRHub does not give IDs to an external plugin. The admin sets them all.
- A plugin has at most 32 ranges, and each ID is a 24-bit DMR ID.

### Run the parrot and the location server as programs

The parrot and the location server are also programs, `dmrhub-parrot` and `dmrhub-locator`, for a hub that does not run plugins inside DMRHub. The release archives and the Docker image include them. To move a hub from the built-in plugins to these programs, follow [Upgrade from a hub with the built-in parrot](#upgrade-from-a-hub-with-the-built-in-parrot). Each program takes the hub's address and fingerprint as flags or environment variables, and its token from the environment:

| Flag           | Environment variable        | Value                                                                                                              |
| -------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `-address`     | `DMRHUB_PLUGIN_ADDRESS`     | The address and port from the plugin's page, such as `hub.example.org:3006`.                                      |
| `-fingerprint` | `DMRHUB_PLUGIN_FINGERPRINT` | The fingerprint from the plugin's page. Leave it empty when the hub has a certificate from a certificate authority. |
|                | `DMRHUB_PLUGIN_TOKEN`       | The token of the plugin.                                                                                           |

A program exits with status 2 when the address or the token is missing. When it loses the hub, it connects again after a wait that grows from 1 second to 30 seconds.

```bash
DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... dmrhub-parrot -address hub.example.org:3006 -fingerprint 4F:2A:...
DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... dmrhub-locator -address hub.example.org:3006 -fingerprint 4F:2A:...
```

The location server keeps nothing on disk. After a restart, it learns each open request from the next report that answers it. It asks a tracked radio for its position the next time the hub hears the radio.

In Docker, the programs are in `/app` of the DMRHub image:

```bash
docker run -e DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... --entrypoint /app/dmrhub-parrot ghcr.io/usa-reddragon/dmrhub \
  -address hub.example.org:3006 -fingerprint 4F:2A:...
```

### Upgrade from a hub with the built-in parrot

Before plugins, DMRHub answered 9990 and 5050 to 5059 itself. With **Run plugins inside DMRHub** on, as by default, the built-in plugins answer those IDs. You do not have to do anything.

To run them as programs instead:

1. Turn off **Run plugins inside DMRHub**, and turn on **Accept plugins that run as their own programs**. Restart DMRHub.
2. Add an external plugin named Parrot with the ID 9990, and run `dmrhub-parrot` with its token.
3. Add an external plugin named Location server with the IDs 5050 to 5059, and run `dmrhub-locator` with its token.

The built-in plugins stay on the plugins page with the status **Off**. When you turn **Run plugins inside DMRHub** on again, they show **Waiting for its IDs** while the external plugins have their IDs.

The upgrade removes the user 9990 (callsign Parrot) and the talkgroup 9990 (DMRHub Parrot) that older versions made, unless an admin changed them. When it removes the talkgroup, it also removes it from the static and dynamic talkgroups of every repeater. The old calls to and from the parrot stay in Last Heard.

### Several replicas

Only one replica runs each plugin inside DMRHub: the replica that holds the plugin's lease in the KV store (Redis). A replica renews its leases every 5 seconds, and a lease lasts 15 seconds. When a replica stops without warning, another replica takes the plugin over in about 15 to 20 seconds. When a replica stops cleanly, it gives up its leases, and another replica starts the plugins a moment later. The calls to their IDs in that gap are lost. DMRHub keeps uploaded plugins in the database, so every replica has them.

A plugin that runs as its own program connects to any one replica. A newer connection of the same plugin ends the older one.

When a replica stops and other replicas run, as in a rolling update, the replica drains its external plugins:

1. It sends a `Draining` message to each external plugin that is connected to it, and refuses new plugin connections.
2. A plugin built with the current SDK opens a second connection, which reaches another replica. It keeps the first connection.
3. When the new connection is up, the plugin sends on it. The old replica ends the old connection about a second later.

A burst that comes on both connections reaches the plugin once. The plugin sees no disconnection, and no call to its IDs is lost. The replica waits for this for as long as it drains its repeaters (`dmr.drain-timeout`, 25 seconds by default). A plugin built with an older SDK ignores `Draining`. It connects again when its connection ends, and the calls to its IDs in that gap are lost.

## How the hub routes plugin calls

- A private call to one of a plugin's IDs goes only to the plugin.
- A group call to one of a plugin's IDs goes to the plugin. When a talkgroup has that ID, its subscribers get the call too.
- A plugin sends calls only from its own IDs. It sends private calls to radios, and group calls only to talkgroups among its own IDs. It never sends to the IDs of another plugin. The hub refuses other bursts with a `Refusal`.
- The hub routes a plugin's calls as it routes the calls of a repeater, with the same limit of one call at a time on each timeslot.
- A group call of a plugin goes to the OpenBridge peers whose rules take it. A private call of a plugin goes to the peers only when the radio is not on this hub, so that a reply to a radio here does not arrive twice.
- A plugin can ask for the radios whose owners turned position tracking on, and for where the hub hears them. It can send the positions that those radios report to its IDs. The hub keeps a position only when the radio's owner turned tracking on. It then shows the position on the map and sends it to APRS-IS as the owner chose.

## Write a plugin

### With the Go SDK

The Go SDK is the package `github.com/USA-RedDragon/DMRHub/sdk/plugin`, under the MIT license (`sdk/LICENSE`). The messages are in `github.com/USA-RedDragon/DMRHub/sdk/plugin/v1`.

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
		Author:      "N0CALL",
		Description: "Logs each radio that calls 9100 during a net and answers with a short tone.",
		Ids:         []*pluginv1.IDRange{{First: 9100, Last: 9100}},
		Hosts:       []*pluginv1.Host{{Address: "nets.example.org:443", Reason: "Posts the check-in list when a net ends."}},
	}, func(ctx context.Context, conn *plugin.Conn) error {
		for {
			m, err := conn.Recv()
			if err != nil {
				return err
			}
			if b := m.GetBurst(); b != nil && plugin.IsVoice(b) {
				// Record the call, then answer it from 9100 with conn.Send.
			}
		}
	})
}
```

`plugin.MainWith` connects the plugin to the hub and calls your function with the connection. When the function returns an error, a plugin inside DMRHub exits and the hub starts it again. A plugin that runs as its own program connects again.

The main parts of the SDK:

| Name                                                  | What it does                                                                                   |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `plugin.MainWith(hello, serve)`                       | Runs the plugin with its manifest. `plugin.Main(serve)` runs it with no manifest.             |
| `conn.Recv()`                                         | Returns the next message from the hub, such as a burst to one of the plugin's IDs.           |
| `conn.Send(burst)`                                    | Sends a burst. It is safe to call from several goroutines.                                    |
| `conn.SubscribeTracking(on)`                          | Asks for the radios with position tracking on, and for where the hub hears them.             |
| `conn.SendPosition(position)`                         | Gives the hub a position that a radio reported to one of the plugin's IDs.                    |
| `conn.Dial(ctx, "host:port")`                         | Opens a TCP connection. Inside DMRHub, only to the hosts that an admin allowed.               |
| `conn.Name()`, `conn.Ranges()`                        | The plugin's name and IDs on the hub.                                                         |
| `plugin.DataOf(burst)`, `plugin.BurstOf(data)`        | Change a burst into a DMRD packet of [dmrgo](https://github.com/USA-RedDragon/dmrgo), and back. |
| `plugin.IsVoice(burst)`, `plugin.IsTerminator(burst)` | Tell whether a burst is part of a voice call, and whether it ends one.                        |

dmrgo has voice and data receivers and transmitters that decode and build calls from DMRD packets. The parrot in `plugins/parrot` and the location server in `plugins/locator` are full examples.

To reach a web API, use `conn.Dial` as the `DialContext` of an `http.Transport`. A plugin inside DMRHub has no certificate authorities of its own, so its TLS client needs a set, such as `golang.org/x/crypto/x509roots/fallback`.

### Build the plugin

To run it inside DMRHub, build a `wasip1` module and upload the `.wasm` file:

```bash
GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags='-s -w' -o net-checkin.wasm .
```

You can also build it with [TinyGo](https://tinygo.org) 0.43.0 or later, as DMRHub builds its built-in plugins. The module is much smaller and uses less memory inside DMRHub:

```bash
tinygo build -target=wasip1 -no-debug -o net-checkin.wasm .
```

TinyGo writes `print`, `println` and its panics on stdout, where DMRHub reads the plugin's messages. Log with `log/slog`, which writes on stderr. When a plugin writes something on stdout that is not a message, DMRHub logs it and stops the plugin.

The same code, built for your computer, runs as its own program. It takes the flags and the environment variables in [Run the parrot and the location server as programs](#run-the-parrot-and-the-location-server-as-programs).

### The manifest

A plugin tells the hub who it is with a `Hello` message. In Go, this is the first argument of `plugin.MainWith`.

| Field         | Rules                                                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`        | Required. 1 to 64 characters, with no space at the start or end. The plugins page shows it.                                                |
| `version`     | Required for an upload. DMRHub cuts it to 32 characters. When it is empty, the Go SDK uses the version of the plugin's Go module.         |
| `author`      | At most 64 characters.                                                                                                                      |
| `description` | What the plugin does, in a sentence or two. At most 500 characters.                                                                         |
| `ids`         | The ID ranges that the plugin asks for, from `first` to `last`, both included. The admin can change them.                                  |
| `hosts`       | For a plugin inside DMRHub: at most 16 hosts that it asks to reach. Each has an `address` as `host:port`, at most 255 characters, and a `reason` of at most 200 characters. |

A plugin that runs as its own program can send its `Hello` after the `Welcome`. The hub then keeps only its version, for the plugins page. The admin sets the name and the IDs.

### The sandbox

A plugin inside DMRHub has these limits:

| Limit        | Value                                                                                                                                                  |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Files        | None.                                                                                                                                                  |
| Environment  | None.                                                                                                                                                  |
| Memory       | 64 MiB.                                                                                                                                                |
| CPU          | Half a core over each 10 seconds.                                                                                                                      |
| Network      | Only the hosts that an admin allowed. At most 4 connections at once. A connection must open within 10 seconds. A read or write that waits 5 minutes ends. |
| Start        | The plugin must send its `Hello` within 10 seconds.                                                                                                    |
| Reading      | The plugin must read what the hub sends within 30 seconds.                                                                                             |
| Message size | 64 KiB from the plugin. 4 MiB from the hub.                                                                                                            |
| Logs         | At most 20 lines of stderr each second go to DMRHub's log.                                                                                             |

A host that an admin allowed by name must have a public address, so that its DNS cannot point the plugin at the hub's own network. An IP address that an admin allowed can be private, such as a device on your LAN. A plugin never reaches loopback, link-local, multicast or unspecified addresses.

A plugin that goes over a limit, or ends, stops. DMRHub starts it again after a wait that doubles from 1 second to 1 minute.

DMRHub compiles plugins to machine code on amd64 and arm64. On other machines, such as a 32-bit Raspberry Pi or riscv64, the wazero interpreter runs them, which is much slower. DMRHub compiles each plugin file once. The plugins that run the same file share its code, and a plugin that starts again does not compile it again. The parrot and the location server are one file.

### The protocol

A plugin in another language uses the messages in `proto/dmrhub/plugin/v1/plugin.proto` (MIT license). The service is `dmrhub.plugin.v1.PluginService`. It has one method, `Connect`, a stream in both directions. The plugin sends `ConnectRequest` messages and the hub sends `ConnectResponse` messages.

- **As its own program,** the plugin opens one `Connect` stream of the gRPC service over TLS. It sends its token in the `authorization` metadata as `Bearer <token>`. The hub answers first with a `Welcome`.
- **Inside DMRHub,** the plugin is a WASI preview 1 (`wasip1`) command. It reads the `ConnectResponse` messages on its stdin and writes the `ConnectRequest` messages on its stdout. Each message is its length, as 4 bytes big-endian, then its protobuf encoding. The first message of the plugin is a `Hello`, and the hub answers with the `Welcome`. The plugin reaches the network with `NetRequest` messages, and the hub answers each with a `NetResponse`. A read or write moves at most 16384 bytes.

What the plugin sends (`ConnectRequest`):

| Message      | Use                                                                                                          |
| ------------ | ------------------------------------------------------------------------------------------------------------ |
| `Hello`      | Who the plugin is and what it asks for. See [The manifest](#the-manifest).                                  |
| `Burst`      | A burst of a call that the plugin sends.                                                                     |
| `Subscribe`  | `tracking: true` asks for the radios with position tracking on. A later `Subscribe` replaces an earlier one. |
| `Position`   | A position that a radio reported to one of the plugin's IDs.                                                 |
| `NetRequest` | Opens, reads, writes or closes a TCP connection. Only inside DMRHub.                                         |

What the hub sends (`ConnectResponse`):

| Message       | Use                                                                                                                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Welcome`     | The first message: the plugin's name and IDs on the hub.                                                                                                                                              |
| `Burst`       | A burst of a call to one of the plugin's IDs.                                                                                                                                                          |
| `Refusal`     | A burst of the plugin that the hub did not send, with the reason: the source is not the plugin's, the destination is not allowed, the burst is malformed, or the plugin sent faster than a timeslot carries. |
| `Ranges`      | The admin changed the plugin's IDs.                                                                                                                                                                    |
| `Tracked`     | After a `Subscribe` with tracking: all the radios with tracking on.                                                                                                                                   |
| `Tracking`    | An owner turned tracking of a radio on or off, or changed how often it reports.                                                                                                                       |
| `Heard`       | The hub heard a tracked radio on a repeater, with its timeslot and color code.                                                                                                                        |
| `Draining`    | The replica stops soon. Connect again while you keep this stream. See [Several replicas](#several-replicas).                                                                                         |
| `NetResponse` | The answer to a `NetRequest`.                                                                                                                                                                          |

A `Burst` is one DMR burst as the MMDVM network protocol carries it. It has the source and destination IDs, group or private call, the timeslot (1 or 2), the stream ID, the frame type, the data type or voice sequence, the sequence number (0 to 255), and the 33 bytes of the burst. Its `repeater` field is the repeater or hotspot that the call came from. The hub ignores that field on the bursts that a plugin sends.
