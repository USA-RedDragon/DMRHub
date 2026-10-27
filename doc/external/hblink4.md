# HBlink4

Tested 2026-10-27 with HBlink4 2348c3b (github.com/n0mjs710/HBlink4) on Python 3.14 with dmr_utils3 0.1.31, against DMRHub 569c6e70.

## What it is

HBlink4 is a new DMR server in Python, from the n0mjs710 GitHub account that also has dmr_utils3. Hotspots and repeaters log in to it, and it routes calls by talkgroup and timeslot. It can also log in to other masters as a repeater (`outbound_connections`) and link to other servers with OpenBridge version 1 (`openbridge_connections`). It logs in to DMRHub with the MMDVM network protocol.

## What to set in DMRHub

For an outbound connection:

1. Enroll the `radio_id` of the connection at `<siteurl>/repeaters/new`, such as `300000107`, and copy the password into `passphrase`.
2. Set the talkgroups in `options`, or leave `options` empty and set them on the repeater page.

For an OpenBridge connection, an admin adds a peer at `<siteurl>/admin/peers/new` with the `network_id` as **Peer ID** and the `local_address` and `local_port` as **IP Address** and **Port**, turns on receive and transmit, leaves enhanced OpenBridge off, and adds the rules of the peer. See [OpenBridge peers](../admin/openbridge.md).

## Configuration

This `config.json` worked. Run it with `python run_hblink.py config.json` from the HBlink4 directory.

```json
{
 "global": {"bind_ipv4": "127.0.0.1", "port_ipv4": 62071, "disable_ipv6": true, "max_missed": 3, "timeout_duration": 30,
  "logging": {"file": "hblink4.log", "console_level": "INFO", "file_level": "DEBUG", "retention_days": 1},
  "stats_interval": 60, "report_stats": false, "stream_timeout": 2.0, "stream_hang_time": 10.0, "user_cache": {"timeout": 600}},
 "dashboard": {"enabled": false},
 "repeater_configurations": {"default": {"passphrase": "passw0rd", "slot1_talkgroups": [91], "slot2_talkgroups": [3100]}},
 "outbound_connections": [{"enabled": true, "name": "DMRHub", "address": "dmrhub.example.org", "port": 62031,
  "passphrase": "<repeater password>",
  "radio_id": 300000107, "callsign": "N0CALL", "rx_frequency": 438800000, "tx_frequency": 438800000, "power": 1, "colorcode": 1,
  "latitude": "38.0000", "longitude": "-95.0000", "height": 0, "location": "Anytown", "description": "HBlink4 test",
  "url": "https://example.org", "software_id": "HBlink4", "package_id": "HBlink4", "options": "TS1=91;TS2=3100;",
  "unit_calls_enabled": true}],
 "openbridge_connections": [{"enabled": true, "name": "DMRHub-OBP", "network_id": 3000051,
  "local_address": "127.0.0.1", "local_port": 62072, "target_address": "dmrhub.example.org", "target_port": 62035,
  "passphrase": "<peer password>", "preserve_source_peer": true, "talkgroup_slots": {"3100": "1"}, "description": "test"}]
}
```

`talkgroup_slots` maps each talkgroup that the OpenBridge link carries to a timeslot. In the run, TG 3100 from OpenBridge went to timeslot 1 (`-> TS1` in the log).

## Check that it works

The HBlink4 log shows the login, and a line for the OpenBridge link that starts with `OpenBridge "DMRHub-OBP" bound 127.0.0.1:62072`:

```text
INFO - [DMRHub] Sent RPTL (login) with ID 300000107
```

DMRHub applies the options at the login (`Applied static talkgroups of repeater options repeaterID=300000107 ts1Static=[91] ts2Static=[3100]`). On `<siteurl>/repeaters/300000107`, **Package ID** and **Software ID** are `HBlink4`.

A call from a DMRHub hotspot on TG 91 (TS1) and on TG 3100 (TS2) reached HBlink4. TG 3100 came on both links:

```text
INFO - [DMRHub] RX stream started TS/TGID: 1/91 src=3000001 from remote repeater 300000107
INFO - [OBP DMRHub-OBP] RX stream start src=3000001 tgid=3100 -> TS1 stream_id=46194515 targets=0
INFO - [DMRHub] RX stream started TS/TGID: 2/3100 src=3000001 from remote repeater 300000107
```

We did not send calls from HBlink4 to DMRHub in this run. The stress battery (`hack/stress.sh`, area `transports`) runs HBlink4 too; there it heard 3 of 3 calls.
