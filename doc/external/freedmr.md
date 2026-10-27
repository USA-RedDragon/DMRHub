# FreeDMR

Tested 2026-10-27 with FreeDMR 546165e (gitlab.hacknix.net/hacknix/FreeDMR) on Python 3.14 with Twisted 26.4.0, against DMRHub 569c6e70. `hack/freedmr-test.sh` passed on the same build.

## What it is

FreeDMR is a DMR network server, based on HBlink. Hotspots and repeaters log in to it, and FreeDMR servers link to each other with an enhanced form of OpenBridge (version 5, with BCKA keepalives and DMRE data packets). DMRHub links to FreeDMR as one more server of that kind.

## What to set in DMRHub

The admin does these steps:

1. Turn on OpenBridge (`dmr.openbridge.enabled`). The default port is 62035.
2. At `<siteurl>/admin/peers/new`, add a peer:
   - **Peer ID**: the `NETWORK_ID` of the OpenBridge system in FreeDMR. In our run this was also the `SERVER_ID` of FreeDMR, `3000`.
   - **IP Address** and **Port**: the `IP` and `PORT` of that system.
   - Turn on **Receive DMR traffic from this peer**, **Transmit DMR traffic to this peer** and **Use FreeDMR enhanced OpenBridge with this peer**.
3. Copy the peer password into `PASSPHRASE`.
4. Add a receive rule and a transmit rule with the API. See [OpenBridge peers](../admin/openbridge.md).

`dmr.openbridge.server-id` sets the source server ID that DMRHub sends to enhanced peers. We left it at 0, which sends the network ID of each peer. That worked with `NETWORK_ID` equal to `SERVER_ID`.

## Configuration

These are the parts of `freedmr.cfg` for the link. The rest came from the configuration of `hack/freedmr-test.sh` (in `internal/dmr/freedmr_test.go`).

```ini
[GLOBAL]
SERVER_ID: 3000
VALIDATE_SERVER_IDS: False

[OBP-DMRHUB]
MODE: OPENBRIDGE
ENABLED: True
IP: 127.0.0.1
PORT: 62081
NETWORK_ID: 3000
PASSPHRASE: <peer password>
TARGET_IP: dmrhub.example.org
TARGET_PORT: 62035
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL
RELAX_CHECKS: True
ENHANCED_OBP: True
PROTO_VER: 5
```

Run it with `python bridge_master.py -c freedmr.cfg`. `bridge_master.py` takes no rules file.

## Check that it works

In our run, a hotspot on DMRHub and a hotspot on FreeDMR both had TG 91 on timeslot 1:

| Step | Result |
| --- | --- |
| Call from DMRHub, before the peer had rules | The FreeDMR hotspot got nothing |
| Call from DMRHub, after the receive and transmit rules | The FreeDMR hotspot got the call |
| Call from the FreeDMR hotspot | The DMRHub hotspot got the call. Last Heard on DMRHub shows it from repeater 3000, the peer |

The FreeDMR log shows `Starting OBP. TARGET_IP: ..., TARGET_PORT: 62035`. Right after the start it can log `not sending to system OBP-DMRHUB as KeepAlive never seen`. It logged this once in our run, and calls crossed after that. FreeDMR stops sending to an enhanced link after 60 seconds without a keepalive; `hack/freedmr-test.sh` checks that calls still cross after a minute.
