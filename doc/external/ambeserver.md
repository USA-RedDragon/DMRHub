# AMBE servers

Tested 2026-10-27 with the `ambeserver` of dmrgo-ambe (commit e7b55e1), a software AMBE server, against DMRHub 569c6e70. An AMBEserver with a USB AMBE device is **untested**, because the test host has no such device.

## What it is

An AMBE server converts DMR voice between AMBE frames and audio. It answers the AMBEserver packet protocol on UDP, usually on port 2460. AMBEserver (github.com/marrold/AMBEServer) puts a USB AMBE device on the network. The `ambeserver` of dmrgo-ambe answers the same protocol with a software codec.

DMRHub uses AMBE servers for transcoding: it converts calls between AMBE and Codec2, so that radios and apps with different codecs hear each other. [Analog_Bridge](analog-bridge.md) can use the same servers as its vocoder.

The dmrgo-ambe repository was not public when we tested (github.com/USA-RedDragon/dmrgo-ambe returned 404), so we built `ambeserver` from a local checkout.

## What to set in DMRHub

The admin adds the servers in the configuration. See [Transcoding](../admin/transcoding.md).

```yaml
dmr:
  transcoding:
    ambe-servers:
      - address: ambeserver.example.org:2460
        calls: 0
```

- DMRHub tries the servers in the order of the list. Each converted call holds one server for its whole length.
- `calls` is the number of calls that a server converts at once. 0 is no limit. The DMRHub configuration reference says to use 1 for an AMBEserver with one chip, which answers only its last client.
- A server must accept the setting that makes it report the decoder status of each frame (`PKT_SPCHFMT`). A server that refuses it converts no call.

Then set the codec of the timeslots that use Codec2. On `<siteurl>/repeaters/<repeater ID>`, in **Voice codec**, set **Codec** of the timeslot to **Codec2**. **Automatic** uses the codec that DMRHub last heard on the timeslot.

## Configuration of ambeserver

This `ambeserver` configuration worked:

```yaml
log-level: info
udp:
  bind: "[::]"
  port: 2460
chip:
  channels: 1
  rate: 33
```

`rate: 33` is the DMR rate, 3600 bit/s with FEC. Run it with `ambeserver --config ambeserver.yaml`.

## Check that it works

`GET <siteurl>/api/v1/config/transcoding` (admin only) lists the servers. `answered_seconds` is the time since the server last answered, and `calls` is the calls that it converts now. Before a call, in our run:

```json
{"servers":[{"server":"127.0.0.1:2460","calls_limit":0,"answered_seconds":0,"calls":0}]}
```

In our run, a hotspot sent a 3-second AMBE call on TG 3100, timeslot 2, and a second hotspot had timeslot 2 set to **Codec2**:

- The `ambeserver` log showed `New client client=127.0.0.1:...` for the call.
- The second hotspot got the call as a voice stream on TG 3100.
- `answered_seconds` was 2 after the call.
