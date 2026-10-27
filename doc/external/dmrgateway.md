# DMRGateway

Tested 2026-10-27 with DMRGateway d751e20 (g4klx/DMRGateway, version 20260713) and MMDVMHost 9384d68, against DMRHub 569c6e70.

## What it is

DMRGateway sits between [MMDVMHost](mmdvmhost.md) and the DMR networks. It logs in to up to five networks at once with the MMDVM network protocol, and sends each call to a network by the rules of its configuration. Use it to connect a hotspot to DMRHub, or to DMRHub and another network at the same time.

## What to set in DMRHub

1. Go to `<siteurl>/repeaters/new`. Enter the hotspot ID in **DMR Radio ID**, such as `300000101`, and click **Save**. See [Connect a hotspot](../guides/connect-hotspot.md).
2. Copy the password from the window that DMRHub shows. DMRHub shows it only once.

The window shows a `[DMR Network 2]` section with `Name=DMRHub` and the `Port` of the MMDVM listener. The name is only a label.

## Configuration

This `DMRGateway.ini` worked. It sends every call to DMRHub as network 1 and sets the static talkgroups with the options text.

```ini
[General]
Id=300000101
Timeout=10
RptAddress=127.0.0.1
RptPort=62032
LocalAddress=127.0.0.1
LocalPort=62037
RuleTrace=0
Daemon=0
Debug=0

[Log]
DisplayLevel=1
MQTTLevel=0

[Voice]
Enabled=0

[Info]
Callsign=N0CALL
RXFrequency=438800000
TXFrequency=438800000
Power=1
ColorCode=1
Duplex=0
Slot1=1
Slot2=1
Latitude=38.0000
Longitude=-95.0000
Height=0
Location=Anytown, KS
Description=DMRGateway test
URL=https://example.org

[XLX Network]
Enabled=0

[DMR Network 1]
Enabled=1
Name=DMRHub
Address=dmrhub.example.org
Port=62031
Id=300000101
Password="Ab3dE6gH9jK2"
PassAllTG=1
PassAllTG=2
PassAllPC=1
PassAllPC=2
Options="TS1=91;TS2=3100;"
Location=1
Debug=0

[DMR Network 2]
Enabled=0
```

Notes from the run:

- `Id` in `[General]` is required. Without it, this DMRGateway stops at start with `Assertion 'id > 0U' failed` (MMDVMNetwork.cpp:52).
- DMRGateway takes the callsign, frequencies, color code and location that it sends to DMRHub from its own `[Info]` section.
- DMRGateway removes the quotes around `Password` and `Options`. Quotes are needed for an options text, because the text has `;` in it.
- `Options` sets the static talkgroups on DMRHub when DMRGateway logs in. DMRHub replaces the static talkgroups of both timeslots with the ones in the text. See [Options text](index.md#options-text). Leave `Options` out to set the talkgroups on the repeater page instead.
- `PassAllTG` and `PassAllPC` send all group and private calls of a timeslot to this network. With more than one network, use `TGRewrite` rules instead, as the sample `DMRGateway.ini` shows.
- DMRGateway logged in about 10 seconds after it started.

## Check that it works

The DMRGateway log shows the configuration that it sends and the options:

```text
I: DMR Network 1 Parameters
I:     Name: DMRHub
I:     Id: 300000101
I:     Options: TS1=91;TS2=3100;
I: DMRHub: configuration message: N0CALL  438800000438800000010138.00000-95.00000000Anytown, KS ...
M: DMRHub, Opening DMR Network
```

The DMRHub log shows the login and the options:

```text
INF Repeater connected repeaterID=300000101 callsign=N0CALL
INF Repeater options repeaterID=300000101 options="TS1=91;TS2=3100;"
INF Applied static talkgroups of repeater options repeaterID=300000101 ts1Static=[91] ts2Static=[3100]
```

On `<siteurl>/repeaters/300000101`, **Protocol** is MMDVM, **Package ID** is `MMDVM`, **Software ID** is the DMRGateway version (`20260713`), and **Last Ping** moves while DMRGateway runs. A call on TG 3100 from another hotspot shows in the [MMDVMHost](mmdvmhost.md) log.
