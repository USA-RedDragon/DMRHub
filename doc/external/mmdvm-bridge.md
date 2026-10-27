# MMDVM_Bridge

Tested 2026-10-27 with MMDVM_Bridge 20210520_V1.6.8 (the `MMDVM_Bridge.amd64` binary of github.com/DVSwitch/MMDVM_Bridge at 285e51e), against DMRHub 569c6e70.

## What it is

MMDVM_Bridge is a part of DVSwitch. It logs in to a DMR master as a hotspot with the MMDVM network protocol, and passes the AMBE voice frames to a partner program over UDP, such as [Analog_Bridge](analog-bridge.md). With Analog_Bridge, it connects DMRHub to analog audio, such as an AllStarLink node.

DVSwitch publishes MMDVM_Bridge as a binary only. There is no source.

## What to set in DMRHub

1. Enroll the `Id` of the `[General]` section at `<siteurl>/repeaters/new`, such as `300000105`, and copy the password.
2. Pick the talkgroup with `Options` in `[DMR Network]`, or set it on the repeater page. MMDVM_Bridge uses only timeslot 2 in this configuration (`Slot1=0`, `Slot2=1`), so put the talkgroup on timeslot 2.

## Configuration

Start from the `MMDVM_Bridge.ini` of the DVSwitch package. Change these keys:

```ini
[General]
Callsign=N0CALL
Id=300000105

[DMR]
Enable=1

[DMR Network]
Enable=1
Address=dmrhub.example.org
Port=62031
Jitter=360
Local=62032
Password=<repeater password>
Options=TS2=3100;
Slot1=0
Slot2=1
Debug=0
```

`Options=TS2=3100;` sets TG 3100 as the static talkgroup of timeslot 2 and clears the static talkgroups of timeslot 1. See [Options text](index.md#options-text).

`DVSwitch.ini` in the same directory sets the UDP ports to the partner program. The defaults of the package (`[DMR]`, `txPort = 31100`, `rxPort = 31103`) match the defaults of Analog_Bridge.

## Check that it works

The MMDVM_Bridge log shows the login and the calls from the network:

```text
M: DMR, Logged into the master successfully: 127.0.0.1:62031
M: DMR Slot 2, received network voice header from 3000001 to TG 3100
M: DMR Slot 2, received network end of voice transmission, 1.6 seconds, 0% packet loss, BER: 0.0%
```

On `<siteurl>/repeaters/300000105`, **Package ID** is `MMDVM_DMO`, **Software ID** is `20210520_V1.6.8`, and the description is `MMDVM_Bridge`. For calls from MMDVM_Bridge to DMRHub, see [Analog_Bridge](analog-bridge.md).
