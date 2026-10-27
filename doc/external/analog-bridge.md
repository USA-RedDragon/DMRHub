# Analog_Bridge

Tested 2026-10-27 with Analog_Bridge 1.6.4 (the `Analog_Bridge.amd64` binary of github.com/DVSwitch/Analog_Bridge at dbc4c58) and [MMDVM_Bridge](mmdvm-bridge.md) 20210520_V1.6.8, against DMRHub 569c6e70. It used an AMBE server on UDP port 2460 as its vocoder.

## What it is

Analog_Bridge is a part of DVSwitch. It converts between the AMBE frames of MMDVM_Bridge and 8 kHz audio in USRP packets. The USRP partner can be Analog_Reflector, AllStarLink (Asterisk) or another Analog_Bridge, as the sample configuration tells. It does not connect to DMRHub itself. The chain is:

DMRHub, then MMDVM_Bridge (MMDVM network protocol), then Analog_Bridge (AMBE frames over UDP), then the USRP partner (audio over UDP).

Analog_Bridge needs an AMBE vocoder. It can use an AMBE server over UDP (the `[DV3000]` section). DVSwitch publishes Analog_Bridge as a binary only.

## What to set in DMRHub

Nothing for Analog_Bridge. Set up [MMDVM_Bridge](mmdvm-bridge.md). Calls from Analog_Bridge come into DMRHub from the hotspot of MMDVM_Bridge, with the source ID in `gatewayDmrId`. In our run, it was 3000001, the DMR ID of the owner of the hotspot.

## Configuration

Start from the `Analog_Bridge.ini` of the DVSwitch package. These are the keys that the run changed or used:

```ini
[GENERAL]
logLevel = 2
exportMetadata = true
transferRootDir = /tmp
subscriberFile = /var/lib/dvswitch/subscriber_ids.csv
useEmulator = false

[AMBE_AUDIO]
address = 127.0.0.1
txPort = 31103
rxPort = 31100
ambeMode = DMR
gatewayDmrId = 3000001
repeaterID = 300000105
txTg = 3100
txTs = 2
colorCode = 1

[USRP]
address = 127.0.0.1
txPort = 32001
rxPort = 31001

[DV3000]
address = 127.0.0.1
rxPort = 2460
```

- `[AMBE_AUDIO]` `txPort` and `rxPort` are the reverse of `[DMR]` in the `DVSwitch.ini` of MMDVM_Bridge.
- `txTg` and `txTs` are the talkgroup and timeslot of calls from the analog side.
- `[USRP]` `txPort` is where Analog_Bridge sends the audio, and `rxPort` is where it listens for audio. Both are 31001 in the sample. Our test program ran on the same host, so we moved `txPort` to 32001.
- `transferRootDir` and `subscriberFile` are the values of the sample. Our run pointed them to a work directory.
- `[DV3000]` is the AMBE server. See [AMBE servers](ambeserver.md).
- Remove the `include = dvsm.macro` line of the sample when you do not have that file.

## Check that it works

In our run, a test program on the USRP ports stood in for AllStarLink:

| Direction | Test | Result |
| --- | --- | --- |
| DMR to analog | A 3-second call on TG 3100 from a DMRHub hotspot | Analog_Bridge sent 144 USRP voice packets with keyup set to port 32001 |
| Analog to DMR | 150 USRP voice packets (3 seconds of a 440 Hz tone) to port 31001, then an unkey packet | Last Heard on DMRHub shows a 3-second call from 3000001 to TG 3100 through repeater 300000105, and the DMRHub hotspot got it with no bit errors |

The AMBE server logged one new client, Analog_Bridge.
