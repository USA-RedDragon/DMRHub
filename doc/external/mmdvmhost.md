# MMDVMHost

Tested 2026-10-27 with MMDVMHost 9384d68 (g4klx/MMDVMHost, binary `MMDVM-Host`) and DMRGateway d751e20, against DMRHub 569c6e70.

## What it is

MMDVMHost runs the MMDVM modem of a hotspot or repeater (an MMDVM_HS hat, an MMDVM repeater board, a DVMega). It sends and receives the radio traffic. Pi-Star runs it.

The current MMDVMHost of g4klx has no direct connection to a DMR master. Its `[DMR Network]` section connects only to [DMRGateway](dmrgateway.md) on the same host, and DMRGateway logs in to DMRHub. The MMDVMHost of Pi-Star has `Address` and `Port` keys in `[DMR Network]` and can connect to a master directly. See [Pi-Star](pi-star.md).

To run a modem board without MMDVMHost, DMRHub can drive it itself. See [the modem](../admin/modem.md).

## Configuration

The program name changed. The current build is `MMDVM-Host` with `MMDVM-Host.ini`. Older builds are `MMDVMHost` with `MMDVM.ini`.

The parts of the configuration that matter for DMRHub are the `[DMR]` section and the `[DMR Network]` section, which points to DMRGateway:

```ini
[General]
Callsign=N0CALL
Id=300000101
Duplex=0

[DMR]
Enable=1
ColorCode=1

[DMR Network]
Enable=1
LocalAddress=127.0.0.1
LocalPort=62032
GatewayAddress=127.0.0.1
GatewayPort=62037
Jitter=360
Slot1=1
Slot2=1
Debug=0
```

`LocalPort` and `GatewayPort` must match `RptPort` and `LocalPort` in the `[General]` section of DMRGateway. Our run used `[Modem] Protocol=null`, which runs MMDVMHost with no modem. A real hotspot uses `Protocol=uart` and the serial port of the board.

## What to set in DMRHub

Nothing for MMDVMHost itself. Enroll the hotspot ID that DMRGateway logs in with, as [DMRGateway](dmrgateway.md) tells.

`Duplex=0` makes a simplex hotspot. It uses only timeslot 2. In our run it did not play the calls that DMRHub sent on timeslot 1. Put the talkgroups of a simplex hotspot on timeslot 2.

## Check that it works

Make a call on a talkgroup of the hotspot. The MMDVMHost log shows the call from the network:

```text
M: DMR Slot 2, received network voice header from 3000001 to TG 3100
M: DMR Slot 2, Talker Alias "N0CALL test"
M: DMR Slot 2, received network end of voice transmission from 3000001 to TG 3100, 1.6 seconds, 0% packet loss, BER: 0.0%
```

We did not test calls from a radio to DMRHub, because the run had no modem.
