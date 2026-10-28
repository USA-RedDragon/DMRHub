# Plugins

A plugin is a program that answers the calls to the DMR IDs that an admin gives it. The parrot (9990) and the location server (5050 to 5059) are plugins. A plugin runs in one of two ways:

- **Inside DMRHub.** DMRHub runs the plugin as a WebAssembly module, in a sandbox. You install nothing else. The parrot and the location server are built in, and an admin can upload other plugins as `.wasm` files. This is on by default.
- **As its own program.** The plugin runs next to DMRHub or on another computer, and connects to the hub over TLS.

This page is for developers who write plugins. To install, upload and manage plugins on a DMRHub server, see [Plugins](admin/plugins.md) in the admin guide.

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

The same code, built for your computer, runs as its own program. It takes the flags and the environment variables in [Run the parrot and the location server as programs](admin/plugins.md#run-the-parrot-and-the-location-server-as-programs).

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
| `Draining`    | The replica stops soon. Connect again while you keep this stream. See [Several replicas](admin/plugins.md#several-replicas).                                                                                         |
| `NetResponse` | The answer to a `NetRequest`.                                                                                                                                                                          |

A `Burst` is one DMR burst as the MMDVM network protocol carries it. It has the source and destination IDs, group or private call, the timeslot (1 or 2), the stream ID, the frame type, the data type or voice sequence, the sequence number (0 to 255), and the 33 bytes of the burst. Its `repeater` field is the repeater or hotspot that the call came from. The hub ignores that field on the bursts that a plugin sends.
