# MMDVM over WebTransport

A web page cannot open a UDP socket, so a browser cannot be an MMDVM hotspot on the usual UDP port. DMRHub has a second listener for this case. It speaks the MMDVM network protocol inside WebTransport, over HTTP/3 and QUIC. This page is for the people who write a browser hotspot or a browser radio that connects to DMRHub.

[dmr-web](https://github.com/USA-RedDragon/dmr-web), a DMR radio in the browser, is the reference client. DMRHub also has a Go test client in `internal/testutils/wt_peer.go`.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance, and `<host>` is its host name.

## How it works

- Each WebTransport session is one MMDVM peer: one hotspot, with one repeater ID. To connect two hotspots, open two sessions.
- Each WebTransport datagram is one packet of the MMDVM network protocol, the same octets that an MMDVM hotspot sends in one UDP packet. There is no other framing.
- The login, the pings and the DMRD packets are the same as on the UDP port. The DMRHub code that handles them is the same code.
- DMRHub also accepts the HBRP dialect in the datagrams, as it does on the UDP port. HBRP is a different protocol from the MMDVM network protocol. This page covers only the MMDVM network protocol, which dmr-web speaks.
- Streams of the session carry a small number of JSON control messages. They exist only for the move to another replica when a replica of DMRHub stops. See [Control streams](#control-streams).

## Turn it on

The listener is off by default. The admin turns it on in the config:

| Key                                  | Default  | Environment variable               | What it does                                                                                         |
| ------------------------------------ | -------- | ---------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `dmr.mmdvm.webtransport.enabled`     | `false`  | `DMR_MMDVM_WEBTRANSPORT_ENABLED`   | Accept browser hotspots and radios over WebTransport                                                 |
| `dmr.mmdvm.webtransport.port`        | `62032`  | `DMR_MMDVM_WEBTRANSPORT_PORT`      | UDP port of the WebTransport listener                                                                |
| `dmr.mmdvm.webtransport.cert-file`   | (empty)  | `DMR_MMDVM_WEBTRANSPORT_CERT_FILE` | TLS certificate file. Empty makes DMRHub use short-lived certificates of its own                      |
| `dmr.mmdvm.webtransport.key-file`    | (empty)  | `DMR_MMDVM_WEBTRANSPORT_KEY_FILE`  | TLS key file of the certificate file                                                                 |
| `dmr.mmdvm.webtransport.allowed-origins` | (empty) | `DMR_MMDVM_WEBTRANSPORT_ALLOWED_ORIGINS` | Origins of the web pages that may open a session, such as `https://radio.example`. Empty allows every origin |

```yaml
dmr:
  mmdvm:
    webtransport:
      enabled: true
      port: 62032
```

- The listener uses the bind address of the MMDVM server, `dmr.mmdvm.bind`.
- The port is a UDP port. It must not be the MMDVM port, `dmr.mmdvm.port`. Open it in the firewall for UDP, as you do for the MMDVM port.
- Set both `cert-file` and `key-file`, or neither. DMRHub refuses a config with only one of them.

### Certificates

DMRHub gets the TLS certificate of the listener in one of two ways.

**Certificates of its own.** When `cert-file` and `key-file` are empty, DMRHub makes its own certificates. They need no certificate authority and no setup.

- Each certificate is a self-signed ECDSA P-256 certificate, with the common name `DMRHub WebTransport`.
- DMRHub derives the key and the certificate from the `secret` of the config. Each replica that has the same `secret` makes the same certificates.
- One certificate is in use for 10 days. It is valid for 13 days from the start of those 10 days, and for 1 hour before them, for clocks that run behind. Browsers accept a certificate by hash only when it is valid for 14 days or less.
- A browser cannot check such a certificate against a certificate authority. It accepts the certificate only when the page gives its SHA-256 hash in the `serverCertificateHashes` option of the `WebTransport` constructor. [Find the endpoint](#find-the-endpoint) tells how to get the hashes.

**Certificate files.** When the admin sets `cert-file` and `key-file`, such as the files that cert-manager mounts, DMRHub presents that certificate. The browser checks it as it checks any HTTPS certificate, so it must be valid for `<host>`. DMRHub reads the files again when their modification time changes, so a renewed certificate is used without a restart. When DMRHub cannot read the new files, it keeps the last certificate that it loaded. With certificate files, DMRHub publishes no hashes.

## Find the endpoint

`GET /api/v1/network/webtransport` tells a client where the listener is.

- The route needs no sign-in and no token. A request with an OAuth access token of any scope also works. See [OAuth Apps](oauth-apps.md).
- It answers any origin with `Access-Control-Allow-Origin: *`, so a page on another site can call it. Send it without credentials. dmr-web uses `fetch(url, { credentials: 'omit', cache: 'no-store' })`.

When the listener is on:

```json
{
  "certificate_hashes": [
    { "algorithm": "sha-256", "value": "<base64 of the 32-octet hash>" },
    { "algorithm": "sha-256", "value": "<base64 of the 32-octet hash>" }
  ],
  "enabled": true,
  "path": "/mmdvm",
  "port": 62032
}
```

| Field                | What it is                                                                                                                                                       |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `enabled`            | `true` when the listener is on                                                                                                                                   |
| `port`               | The UDP port of the listener                                                                                                                                     |
| `path`               | The path of the WebTransport endpoint, always `/mmdvm`                                                                                                           |
| `certificate_hashes` | With DMRHub's own certificates: two entries, the hash of the certificate in use now and of the next one. With certificate files: an empty list. `value` is the SHA-256 hash of the DER certificate in standard base64, with padding |

When the listener is off, the answer is `{"enabled":false}`. When DMRHub cannot load the certificate, the answer is `500` with `{"error":"Error getting the WebTransport certificate"}`.

The answer has no host name. Use the host name of `<siteurl>`.

The hashes change every 10 days, and the answer always holds the next one too. Get the answer again before each new connection, as dmr-web does, and do not keep the hashes for longer than that.

## Connect

Open the session at `https://<host>:<port><path>`:

```js
const info = await (await fetch('<siteurl>/api/v1/network/webtransport', { credentials: 'omit', cache: 'no-store' })).json();
if (!info.enabled) {
  throw new Error('This DMRHub does not accept browser radios');
}
const hashes = info.certificate_hashes.map((h) => ({
  algorithm: h.algorithm,
  value: Uint8Array.from(atob(h.value), (c) => c.charCodeAt(0)),
}));
const wt = new WebTransport(`https://<host>:${info.port}${info.path}`, hashes.length ? { serverCertificateHashes: hashes } : undefined);
await wt.ready;
```

- The listener accepts TLS 1.3 only. QUIC uses TLS 1.3, so a browser always does.
- When `allowed-origins` is empty, DMRHub does not check the `Origin` of the request, and any page can open a session. Then the login with the hotspot password is the only access check.
- When `allowed-origins` has entries, DMRHub opens a session only for a request whose `Origin` is one of them, and answers `403` to any other request, also to one without an `Origin`. Write each entry as `scheme://host` or `scheme://host:port`, with `http` or `https` and no path. DMRHub compares the scheme and the host without regard to case, and treats port 443 of `https` and port 80 of `http` as no port. DMRHub refuses a config with an entry that is not such an origin.
- DMRHub answers the request to open a session with:

| Status | When                                                                                                                                          |
| ------ | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `200`  | The session is open                                                                                                                           |
| `400`  | The request is not a WebTransport request                                                                                                     |
| `403`  | `allowed-origins` is set, and the `Origin` of the request is not one of them                                                                  |
| `404`  | The path is not `/mmdvm`                                                                                                                      |
| `503`  | This replica of DMRHub drains and takes no new sessions. Try again; the load balancer can send the new session to another replica. See [When a replica stops](#when-a-replica-stops) |

Browsers keep only a few datagrams that the page has not read, and drop the rest. dmr-web comments that Chromium keeps one and Firefox five by default. DMRHub can send many datagrams close together: after a move to another replica it sends them 2 ms apart. Make the buffer larger before you read. dmr-web sets `datagrams.incomingMaxBufferedDatagrams` (or `incomingHighWaterMark` in browsers that have that name) to `1024`.

## Log in

The login is the login of the MMDVM network protocol, in datagrams. The client is the repeater, and DMRHub is the master.

### Before the login

The hotspot must be a hotspot in DMRHub, with a password:

- A user adds it on the new hotspot page of DMRHub, which shows its password when it adds the hotspot.
- An OAuth app adds it with `POST /api/v1/repeaters` and the `hotspots:register` scope. DMRHub answers the password once, in the answer to that request. See [OAuth Apps](oauth-apps.md).

The OAuth access token never goes over WebTransport. DMRHub checks only the repeater ID and the password, as it does for a hotspot on the UDP port. An app uses its token to add the hotspot and to read the password, and then logs in with the password.

DMRHub refuses the login of an ID that is not a hotspot, of a hotspot with no password, and of the ID of the modem of the server. It looks up at most 5 logins of unknown IDs each second, and refuses the others at once.

### The steps

All numbers are big-endian. The repeater ID is 4 binary octets.

| Step | The client sends                                                                         | DMRHub answers                                                         |
| ---- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| 1    | `RPTL` and the ID (8 octets)                                                             | `RPTACK` and a random 4-octet salt (10 octets)                         |
| 2    | `RPTK`, the ID, and the SHA-256 hash of the 4 salt octets followed by the password (40 octets) | `RPTACK` and the ID (10 octets)                                  |
| 3    | `RPTC`, the configuration (302 octets)                                                   | `RPTACK` and the ID                                                    |
| 4    | Optional: `RPTO`, the ID, and the options text                                           | `RPTACK` and the ID                                                    |

The login is complete after the `RPTACK` of step 3. DMRHub answers `MSTNAK` and the ID (10 octets) when it refuses a step: a wrong password, an unknown ID, an `RPTC` that it cannot accept, or a packet of a step that the session has not reached.

For hotspot `312345601` (`0x129E0401`), step 1 is these 8 octets:

```text
52 50 54 4c 12 9e 04 01        RPTL, ID 312345601
```

DMRHub answers, with a salt of `0xA1B2C3D4`:

```text
52 50 54 41 43 4b a1 b2 c3 d4  RPTACK, salt a1b2c3d4
```

Step 2 is `RPTK`, the ID, and `SHA-256(a1 b2 c3 d4 || password octets)`.

Timers of the login:

- The client must finish the login within 15 seconds of its first `RPTL`. After the `RPTK` is accepted, a new period of 15 seconds starts for the `RPTC`. When the time ends, DMRHub ends the session without a packet.
- A second `RPTL` for the same ID while DMRHub waits for the `RPTK` gets the same salt again. It does not restart the timer.
- An `RPTL` for another ID, or after the login, ends the old login and starts a new one.

### The RPTC

The `RPTC` is 302 octets. The text fields are ASCII. Pad them with spaces or NUL octets; DMRHub removes leading and trailing spaces and NULs.

| Octets  | Field                                                         |
| ------- | ------------------------------------------------------------- |
| 0-3     | `RPTC`                                                        |
| 4-7     | Repeater ID                                                   |
| 8-15    | Callsign                                                      |
| 16-24   | RX frequency in Hz, 9 digits                                  |
| 25-33   | TX frequency in Hz, 9 digits                                  |
| 34-35   | Power in dBm, 2 digits                                        |
| 36-37   | Color code, 2 digits                                          |
| 38-45   | Latitude, 8 characters                                        |
| 46-54   | Longitude, 9 characters                                       |
| 55-57   | Antenna height in meters, 3 digits                            |
| 58-77   | Location                                                      |
| 78-96   | Description                                                   |
| 97      | Slots: `1` or `2` for one slot, `3` for both, `4` for simplex |
| 98-221  | URL                                                           |
| 222-261 | Software ID                                                   |
| 262-301 | Package ID                                                    |

Only the callsign can make DMRHub refuse the `RPTC`. It must be 4 to 8 letters and digits after DMRHub makes it upper case. DMRHub logs any other field that it cannot read and stores it as zero. dmr-web sends its callsign, color code 1, slots `3`, its software and package ID, and the description `dmr-web radio`.

### The options

The `RPTO` options text sets the static talkgroups and the default talkgroup of each slot, in the form that FreeDMR reads, such as `TS1=8;TS2=1`. A text with static talkgroup keys replaces the static talkgroups of both slots, and a slot that the text does not name is cleared. The default dial of a slot (`DIAL` and its aliases) links that talkgroup to the slot, and `0` unlinks it. DMRHub logs and ignores the other options, and ignores a text longer than 255 octets.

## Packets after the login

### What the client sends

| Packet    | What DMRHub does                                                                                               |
| --------- | -------------------------------------------------------------------------------------------------------------- |
| `DMRD`    | Routes the burst. It takes 53, 55 and 71 octets. It drops a burst with destination 0                           |
| `DMRA`    | Records the talker alias                                                                                       |
| `DMRG`    | Records the GPS position                                                                                       |
| `RPTPING` | Answers `MSTPONG` and the ID (11 octets). `RPTPING` is `RPTPING` and the ID (11 octets)                         |
| `RPTO`    | Answers `RPTACK` and applies the options again                                                                 |
| `RPTC`    | Stores the configuration again and answers `RPTACK`, or answers `MSTNAK` and ends the session                  |
| `RPTCL`   | Ends the session. `RPTCL` is `RPTCL` and the ID (9 octets)                                                     |

A `DMRD`, `DMRA`, `DMRG` or ping before the login is complete, or with another repeater ID, gets `MSTNAK`. When a slot sends more bursts than DMRHub takes, DMRHub drops the extra bursts.

The `DMRD` layout, as DMRHub sends it (55 octets):

| Octets | Field                                                                                                           |
| ------ | --------------------------------------------------------------------------------------------------------------- |
| 0-3    | `DMRD`                                                                                                          |
| 4      | Sequence number                                                                                                 |
| 5-7    | Source radio ID                                                                                                 |
| 8-10   | Destination talkgroup or radio ID                                                                               |
| 11-14  | Repeater ID                                                                                                     |
| 15     | Bit 7: slot (0 is slot 1, 1 is slot 2). Bit 6: private call. Bits 5-4: frame type (0 voice, 1 voice sync, 2 data sync). Bits 3-0: data type, or the voice burst A to F as 0 to 5 |
| 16-19  | Stream ID                                                                                                       |
| 20-52  | The 33-octet burst                                                                                              |
| 53     | BER                                                                                                             |
| 54     | RSSI                                                                                                            |

### What DMRHub sends

| Packet    | When                                                                                                    |
| --------- | ------------------------------------------------------------------------------------------------------- |
| `DMRD`    | Each burst that DMRHub routes to the hotspot, 55 octets                                                 |
| `MSTPONG` | The answer to each ping                                                                                 |
| `RPTSBKN` | Once, 1 minute after the login: a request for a beacon. `RPTSBKN` and the ID (11 octets)                |
| `MSTNAK`  | A packet that the session does not accept. Log in again                                                 |
| `MSTCL`   | DMRHub stops and no other replica serves. `MSTCL` and the ID (9 octets). Log in again later             |

### Timers

- DMRHub ends the session when it gets no packet from the hotspot for 60 seconds. Every packet that it accepts restarts this time. It checks once each second, and it sends nothing when it ends the session; the next packet of the client gets `MSTNAK`.
- Send `RPTPING` every 10 seconds. dmr-web uses the session of the dmrgo library with its defaults: a ping every 10 seconds, the same time between two sends of a login step, and a new login after 60 seconds without an acknowledgement or a pong.
- The WebTransport session stays open when the MMDVM session ends. The client can log in again on the same session.

## Control streams

Control messages are JSON objects, one per stream, each on one line that ends with a newline (`\n`). They are never in datagrams.

### Resume request

The client opens a bidirectional stream, writes one line, and reads one line back:

```json
{"resume":"<token>"}
```

DMRHub answers:

```json
{"resumed":true}
```

or, when it does not take the session over:

```json
{"resumed":false,"reason":"bad token"}
```

| `reason`                          | Meaning                                                                                                 |
| --------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `bad request`                     | The line is not JSON, or has no `resume`                                                                |
| `bad token`                       | The token is not one that the replicas made, or it expired                                              |
| `no session`                      | No WebTransport session of the hotspot of the token is logged in                                        |
| `the session is not handed over`  | This replica could not take the hotspot over from the replica that holds it                             |
| `the token was used`              | A replica already took a session over with this token                                                   |
| `stopping`                        | This replica stops                                                                                      |
| `busy`                            | This WebTransport session already has an MMDVM session                                                  |
| `not resumed`                     | The takeover did not complete                                                                           |

A token moves a session only once. A refusal does not spend the token, so the client can try it again on another session until it expires.

DMRHub stops reading after the first newline or after 1024 octets, and closes the stream after its answer. It gives up on a stream that sends nothing for 5 seconds.

### Drain notice

DMRHub opens a unidirectional stream to the client, writes one line, and closes the stream:

```json
{"drain":true,"resume":"<token>"}
```

| Field    | What it is                                                                                                      |
| -------- | --------------------------------------------------------------------------------------------------------------- |
| `drain`  | `true`: this replica stops. Move to another one                                                                 |
| `resume` | The resume token. It is good for 1 minute, and for one takeover. Treat it as an opaque string                   |

Read every unidirectional stream that DMRHub opens, and ignore a message that you do not know.

## When a replica stops

A DMRHub with several replicas behind a load balancer moves browser hotspots off a replica that stops, without a new login and without a gap in a call. This happens only when other replicas run. A DMRHub that stops while no other replica serves sends `MSTCL` and closes each session with the reason `server stopping`; then the client connects again later. When other replicas serve, the stopping replica sends no `MSTCL`.

When a replica drains:

1. It answers `503` to each new session, so the load balancer sends new sessions to the other replicas.
2. It sends each logged-in browser hotspot a [drain notice](#drain-notice) with a resume token. It tries for at most 2 seconds.
3. The client opens a second WebTransport session to the same URL, with the same hashes. The load balancer sends it to a replica.
4. The client sends a [resume request](#resume-request) with the token on the new session. It does not log in.
5. When the answer is `{"resumed":true}`, the new session carries the MMDVM session. The client sends its packets on the new session, and closes the old one after a short time. dmr-web closes it after 1 second.
6. When the session does not open, answers `503`, or the answer is not `resumed`, close that session and try again. dmr-web tries 100 times, 200 ms apart. When no try works, stay on the old session until it closes, and then log in again.

The new replica sends the hotspot the bursts that the old replica sent to it in the last 500 ms before the move and after it, in order, 2 ms apart. Some of them can come twice: once on the old session and once on the new one. So:

- Read the datagrams of the new session from the moment it opens. The bursts can come before the answer to the resume request.
- While the move goes on, keep the `DMRD` packets of the old session aside. Drop them when the new session resumes. Use them when the move fails.
- Drop a `DMRD` packet that came before. dmr-web and the Go test client remember each burst for 2 seconds by its slot, its stream ID and its sequence number.

A load balancer can also move the QUIC connection of a session to another path while the replica stays up. Then DMRHub sends again the bursts of the last 500 ms, 2 ms apart, on the same session. The same check for copies drops them.

## Limits

| Limit                                  | Value                                                                    |
| -------------------------------------- | ------------------------------------------------------------------------ |
| Packet in one datagram                 | 512 octets at most. DMRHub drops a longer datagram, as it drops a longer UDP packet |
| Control message                        | 1024 octets, with its newline                                            |
| Wait for a control stream              | 5 seconds to open, read or write a control stream                        |
| Drain notices                          | Sent within 2 seconds                                                    |
| Resume token                           | Good for 1 minute and for one takeover                                   |
| Login                                  | 15 seconds after the first `RPTL`, and 15 seconds after the `RPTK`       |
| Session timeout                        | 60 seconds without a packet                                              |
| Sessions that are not logged in        | 1024 for each DMRHub replica, for the UDP port and WebTransport together |
| Size of a QUIC packet at the start     | 1200 octets                                                              |

The longest packet of a client is the 302-octet `RPTC`. A `DMRD` packet is 55 octets.

## Rules for client authors

- Get `GET /api/v1/network/webtransport` again before each connection, and pass `serverCertificateHashes` only when the list is not empty.
- Make the buffer of incoming datagrams larger before you read.
- Log in with the hotspot password, not with an OAuth token.
- Send a ping every 10 seconds.
- Read the unidirectional streams, and move to another replica on a drain notice.
- Drop copies of `DMRD` packets.
- When the session closes without a drain notice, connect again with a growing wait. dmr-web waits 1 second, then twice as long each time, up to 30 seconds.
