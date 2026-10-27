# HBlink3

Tested 2026-10-27 with HBlink3 1db2730 (github.com/HBLink-org/hblink3) on Python 3.14 with Twisted 26.4.0, bitarray 3.12.3 and dmr_utils3 0.1.31, against DMRHub 569c6e70.

## What it is

HBlink3 is a DMR server and bridge in Python. It calls its protocol HomeBrew, but when it logs in to DMRHub it uses the MMDVM network protocol: DMRHub shows **Protocol** MMDVM for it. Each system in its configuration has a mode:

- `PEER`: HBlink3 logs in to a master, such as DMRHub, as a repeater.
- `MASTER`: hotspots and repeaters log in to HBlink3.
- `OPENBRIDGE`: HBlink3 links to another server, such as DMRHub, with OpenBridge version 1.

`bridge.py` moves calls between the systems by the rules in `rules.py`.

DMRHub does not log in to other masters. To join an HBlink3 master and DMRHub, run HBlink3 with a `PEER` or `OPENBRIDGE` system that points to DMRHub, and bridge it to the `MASTER` system.

## What to set in DMRHub

For a `PEER` system:

1. Enroll the `RADIO_ID` of the system at `<siteurl>/repeaters/new`, such as `300000102`, and copy the password.
2. Set the static talkgroups of the repeater at `<siteurl>/repeaters/300000102`, or put them in `OPTIONS`. With an empty `OPTIONS`, HBlink3 sends `Type=HBlink;`, which has no talkgroups, so DMRHub keeps the static talkgroups of the repeater page.

For an `OPENBRIDGE` system, an admin does this:

1. At `<siteurl>/admin/peers/new`, add a peer. **Peer ID** is the `NETWORK_ID` of the system, **IP Address** and **Port** are the `IP` and `PORT` of the system. Turn on **Receive DMR traffic from this peer** and **Transmit DMR traffic to this peer**. Leave **Use FreeDMR enhanced OpenBridge with this peer** off.
2. Copy the peer password.
3. Add a receive rule and a transmit rule with the API. A peer without rules carries no traffic. See [OpenBridge peers](../admin/openbridge.md).

## Configuration

This `hblink.cfg` worked, with DMRHub at 127.0.0.1. It has all three modes: a `PEER` system and an `OPENBRIDGE` system that point to DMRHub, and a `MASTER` system for local hotspots.

```ini
[GLOBAL]
PATH: ./
PING_TIME: 5
MAX_MISSED: 3
USE_ACL: True
REG_ACL: PERMIT:ALL
SUB_ACL: DENY:1
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL

[REPORTS]
REPORT: False
REPORT_INTERVAL: 60
REPORT_PORT: 4321
REPORT_CLIENTS: 127.0.0.1

[LOGGER]
LOG_FILE: /dev/null
LOG_HANDLERS: console
LOG_LEVEL: INFO
LOG_NAME: HBlink

[ALIASES]
TRY_DOWNLOAD: False
PATH: ./
PEER_FILE: peer_ids.json
SUBSCRIBER_FILE: subscriber_ids.json
TGID_FILE: talkgroup_ids.json
PEER_URL: https://www.radioid.net/static/rptrs.json
SUBSCRIBER_URL: https://www.radioid.net/static/users.json
STALE_DAYS: 7

[DMRHUB-OBP]
MODE: OPENBRIDGE
ENABLED: True
IP: 127.0.0.1
PORT: 62051
NETWORK_ID: 3000050
PASSPHRASE: <peer password>
TARGET_IP: 127.0.0.1
TARGET_PORT: 62035
BOTH_SLOTS: True
USE_ACL: True
SUB_ACL: DENY:1
TGID_ACL: PERMIT:ALL

[DMRHUB]
MODE: PEER
ENABLED: True
LOOSE: False
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: 62052
MASTER_IP: 127.0.0.1
MASTER_PORT: 62031
PASSPHRASE: <repeater password>
CALLSIGN: N0CALL
RADIO_ID: 300000102
RX_FREQ: 438800000
TX_FREQ: 438800000
TX_POWER: 1
COLORCODE: 1
SLOTS: 3
LATITUDE: 38.0000
LONGITUDE: -95.0000
HEIGHT: 0
LOCATION: Anytown
DESCRIPTION: HBlink3 test
URL: https://example.org
SOFTWARE_ID: 20170620
PACKAGE_ID: MMDVM_HBlink
GROUP_HANGTIME: 5
OPTIONS:
USE_ACL: True
SUB_ACL: DENY:1
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL

[LOCAL]
MODE: MASTER
ENABLED: True
REPEAT: True
MAX_PEERS: 4
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: 62053
PASSPHRASE: passw0rd
GROUP_HANGTIME: 5
USE_ACL: True
REG_ACL: PERMIT:ALL
SUB_ACL: DENY:1
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
```

An older peer or repeater password can have a `%`. HBlink3 reads `%` as the start of a variable, so write each `%` as `%%` in `PASSPHRASE`.

This `rules.py` bridges TG 91 on timeslot 1 between DMRHub and the local hotspots, and TG 3100 between the OpenBridge link and timeslot 2 of the local hotspots:

```python
BRIDGES = {
    'TG91': [
        {'SYSTEM': 'DMRHUB', 'TS': 1, 'TGID': 91, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'LOCAL',  'TS': 1, 'TGID': 91, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
    'TG3100': [
        {'SYSTEM': 'DMRHUB-OBP', 'TS': 1, 'TGID': 3100, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'LOCAL',      'TS': 2, 'TGID': 3100, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
}
UNIT = []
```

Run it with `python bridge.py -c hblink.cfg -r rules.py`. Our run used `TRY_DOWNLOAD: False` and empty ID files, because it had no network.

## Check that it works

The HBlink3 log shows the login to DMRHub:

```text
INFO (DMRHUB) Repeater Login ACK Received with 32bit ID: ...
INFO (DMRHUB) Connection to Master Completed with options
```

These four calls all crossed in our run:

| Call | Path | Result |
| --- | --- | --- |
| TG 91, TS1, from a DMRHub hotspot | DMRHub, `PEER` system, `LOCAL` | `*GROUP CALL START*` on `DMRHUB`; the local hotspot got it |
| TG 91, TS1, from a local hotspot | `LOCAL`, `PEER` system, DMRHub | Last Heard on DMRHub shows it from repeater 300000102 |
| TG 3100, TS1, from a DMRHub hotspot | DMRHub, OpenBridge, `LOCAL` TS2 | `Call Bridged to HBP System: LOCAL TS: 2, TGID: 3100` |
| TG 3100, TS2, from a local hotspot | `LOCAL`, OpenBridge, DMRHub | Last Heard on DMRHub shows it from peer 3000050 |

The TG 91 calls also went to `DMRHUB-OBP`. A transmit rule of a peer matches the source radio ID of a call, not its talkgroup, so a peer gets every call from the radios that its rules cover.

## Known problems of HBlink3

- A talker alias (DMRA) from a hotspot on a `MASTER` system makes an exception in hblink.py:532 (`_rf_src` is not set). The call still crosses.
