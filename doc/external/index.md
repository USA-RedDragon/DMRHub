# External software

These pages tell how to connect other DMR programs to DMRHub. Each page tells what the program is, gives a configuration that worked, tells what to set in DMRHub, and tells how to check that the connection works.

Every page comes from a run of the program against a local DMRHub (main at 569c6e70) on 2026-10-27, with all programs and DMRHub on 127.0.0.1 in a network namespace that has only loopback. The [Pi-Star](pi-star.md) page comes from a real Pi-Star hotspot on 2026-10-28 instead. A page that we could not run says why and is marked **Untested**.

The example IDs, callsigns and passwords are not real. Replace them with your own.

## Programs

| Program | What it does | DMRHub listener | Result |
| --- | --- | --- | --- |
| [MMDVMHost](mmdvmhost.md) | Runs the modem of a hotspot or repeater | none (through DMRGateway) | Worked |
| [DMRGateway](dmrgateway.md) | Connects MMDVMHost to one or more DMR networks | MMDVM, UDP 62031 | Worked |
| [Pi-Star](pi-star.md) | Hotspot image with MMDVMHost, DMRGateway and a dashboard | MMDVM, UDP 62031 | Worked through DMRGateway, on a real hotspot. Direct mode not run. |
| [DroidStar](droidstar.md) | DMR client app for phones and desktops | MMDVM, UDP 62031 | Worked |
| [HBlink3](hblink3.md) | DMR server and bridge in Python | MMDVM, UDP 62031, and OpenBridge, UDP 62035 | Worked |
| [HBlink4](hblink4.md) | New DMR server and bridge in Python | MMDVM, UDP 62031, and OpenBridge, UDP 62035 | Worked |
| [FreeDMR](freedmr.md) | DMR network server | OpenBridge, UDP 62035 | Worked |
| [MMDVM_Bridge](mmdvm-bridge.md) | DVSwitch bridge between DMR and other modes | MMDVM, UDP 62031 | Worked |
| [Analog_Bridge](analog-bridge.md) | DVSwitch bridge between digital voice and analog audio | none (through MMDVM_Bridge) | Worked |
| [YSF2DMR](ysf2dmr.md) | Bridge from System Fusion to DMR | MMDVM, UDP 62031 | Login worked |
| [ipsc2mmdvm](ipsc2mmdvm.md) | Bridge from a Motorola IPSC repeater to MMDVM networks | MMDVM, UDP 62031 | Login worked |
| [AMBE servers](ambeserver.md) | Convert calls between AMBE and Codec2 for transcoding | none (DMRHub is the client) | Worked |

## The protocol

Hotspots, repeaters and servers connect to DMRHub with the MMDVM network protocol. This is the protocol that DMRGateway speaks to BrandMeister, TGIF and other masters. DMRHub also accepts logins in the form of the HomeBrew Repeater Protocol specification (HBRP), but none of the programs on these pages uses that form. HBlink3 and HBlink4 call themselves HomeBrew servers, but they log in to DMRHub with the MMDVM protocol.

DMRHub shows the protocol of a connected repeater as **Protocol** on its page, `<siteurl>/repeaters/<repeater ID>`, and as `dialect` in `GET <siteurl>/api/v1/repeaters/<repeater ID>`. In every run on these pages, it was `MMDVM`.

Servers also link to DMRHub with OpenBridge, on its own port. DMRHub accepts OpenBridge version 1, as HBlink uses it, and the enhanced OpenBridge of FreeDMR.

## What to set in DMRHub

For a program that logs in as a hotspot or repeater:

1. Go to `<siteurl>/repeaters/new`, enter the ID in **DMR Radio ID** and click **Save**. See [Connect a hotspot](../guides/connect-hotspot.md).
2. Copy the password that DMRHub shows. DMRHub shows it only once.
3. Pick the talkgroups. You can set static talkgroups on the repeater page, or let the program send them in its options text. See [Options text](#options-text).

For a server that links with OpenBridge, an admin adds a peer at `<siteurl>/admin/peers/new` and then adds its rules with the API. A peer with no rules carries no traffic. See [OpenBridge peers](../admin/openbridge.md).

The admin must turn on the listeners that a program needs. The MMDVM listener is always on. OpenBridge (`dmr.openbridge.enabled`) and IPSC (`dmr.ipsc.enabled`) are off by default. See [Configuration](../admin/configuration.md).

## Options text

DMRGateway, DroidStar, HBlink4, MMDVM_Bridge and other clients can send an options text after they log in, such as `TS1=91;TS2=3100;`. DMRHub reads it as FreeDMR does:

- `TS1=` and `TS2=` set the static talkgroups of each timeslot, as a list with commas, such as `TS1=91,3100;`. They replace the static talkgroups of the repeater. When the text sets one timeslot, DMRHub clears the static talkgroups of the other timeslot. A text with no `TS1` or `TS2` key leaves the static talkgroups as they are.
- DMRHub uses only talkgroups that exist on DMRHub. It skips the others.
- `DIAL=` links a talkgroup as the dynamic talkgroup. `DIAL=0` unlinks it.
- DMRHub logs and ignores the other FreeDMR options, such as `TIMER` and `VOICE`.

MMDVMHost with `Duplex=0` (a simplex hotspot) uses only timeslot 2. In our runs, it did not play calls on timeslot 1, though DMRHub sent them. Put the talkgroups of such a hotspot on `TS2`. DroidStar showed calls on both timeslots.

## Check that a program works

1. Open `<siteurl>/repeaters/<repeater ID>`. **Last Connected** and **Last Ping** must show the current time. **Software ID** and **Package ID** show the program that logged in.

   <!-- screenshot: external-repeater-dmrgateway.png | /repeaters/300000101 while DMRGateway 20260713 is connected: Protocol MMDVM, Last Connected and Last Ping now, Package ID MMDVM, Software ID 20260713, static talkgroups 91 on TS1 and 3100 on TS2 -->

2. Make a call on a talkgroup of the repeater from another radio or hotspot. The program must log the call, and the call must show in **Last Heard**.
3. For an OpenBridge peer, make a call on a talkgroup that the peer carries, and check the log of the server.

## Run the programs yourself

`hack/external/setup.sh` fetches and builds the programs on these pages at the commits that we tested, into `${XDG_CACHE_HOME:-~/.cache}/dmrhub-ext` or the directory you give it:

```bash
hack/external/setup.sh
hack/external/setup.sh -d /srv/dmrhub-ext hblink3 dmrgateway
```

It writes `PINS.md` in the directory with each program and commit. The directory is the one that the external client tests use:

```bash
DMRHUB_STRESS_EXT_DIR=~/.cache/dmrhub-ext DMRHUB_STRESS_AREAS=transports hack/stress.sh
hack/freedmr-test.sh ~/.cache/dmrhub-ext/freedmr ~/.cache/dmrhub-ext/venv/bin/python
hack/droidstar-test.sh ~/.cache/dmrhub-ext/droidstar/build/DroidStar
```

Some of these programs download ID lists from public hosts when they start. Run them in a network namespace with only loopback (`unshare -r -n`) when you test, as the test scripts do.
