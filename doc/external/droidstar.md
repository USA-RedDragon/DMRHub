# DroidStar

Tested 2026-10-27 with a desktop Linux build of DroidStar 0580c7d (github.com/USA-RedDragon/DroidStar, a fork of nostar/DroidStar), against DMRHub 569c6e70.

## What it is

DroidStar is a client app for Android, iOS and desktops. In DMR mode, it logs in to a master as a hotspot with the MMDVM network protocol, and you talk through the phone or computer. It needs an AMBE vocoder to send and play audio: a USB AMBE device, or a software vocoder in some builds.

The fork at 0580c7d has a fix that the upstream nostar/DroidStar did not have when we tested: it logs in again after the master sends MSTCL (the master closes) or MSTNAK (the master refuses). From the commit subject, "fix(dmr): MSTCL/MSTNAK reconnect never fires", we infer that the upstream app stays disconnected after a DMRHub restart. We did not run the upstream app.

Our build had no vocoder, so it showed the calls but did not play them. We did not test sending audio.

## What to set in DMRHub

1. Pick the ID. DroidStar logs in with your DMR ID when **ESSID** is `None`, or with your DMR ID times 100 plus the ESSID minus 1 when you pick an ESSID. For DMR ID `3000001`, ESSID `2` logs in as `300000101`.
2. Enroll that ID at `<siteurl>/repeaters/new` and copy the password. See [Connect a hotspot](../guides/connect-hotspot.md).

## Configuration

DroidStar reads its DMR masters from `DMRHosts.txt` in its settings directory (`~/.config/dudetronics/` on Linux). It downloads that file from a public host when it starts. The run used this file, which DroidStar keeps when the download fails:

```text
# Name ID Address Password Port
DMRHub 1 dmrhub.example.org Ab3dE6gH9jK2 62031
```

The settings that DroidStar used, from `~/.config/dudetronics/droidstar.ini`:

```ini
[General]
MODE=DMR
DMRHOST=DMRHub
CALLSIGN=N0CALL
DMRID=3000001
ESSID=2
BMPASSWORD=
DMRLAT=38.0000
DMRLONG=-95.0000
DMRLOC=Anytown
DMRDESC=DroidStar test
DMRFREQ=438800000
DMRURL=https://example.org
DMROPTS="TS1=91;TS2=3100;"
DMRTGID=91
```

In the app, these are **Callsign**, **DMRID**, **ESSID**, **Latitude**, **Location** and **DMR+ Opts** on the settings tab, and the host and talkgroup on the main tab. **DMR+ Opts** is the options text that DMRHub reads; see [Options text](index.md#options-text).

If you edit `droidstar.ini` by hand, put quotes around `DMROPTS`. Without them, Qt reads the `;` as the start of a comment, and DroidStar sends only `TS1=91`. DMRHub then clears the static talkgroups of timeslot 2, because the text names only timeslot 1.

The **Hosts** tab of the app takes custom hosts in the form `DMR <name> <host> <port> <password>`. We did not get this form to connect in our run, which had no host file. Use the host file, or check the log tab of the app.

## Check that it works

DroidStar logged in about 2.6 seconds after it started. The DMRHub log shows the login and the options:

```text
INF Repeater options repeaterID=300000101 options="TS1=91;TS2=3100;"
INF Applied static talkgroups of repeater options repeaterID=300000101 ts1Static=[91] ts2Static=[3100]
```

On `<siteurl>/repeaters/300000101`, **Protocol** is MMDVM, **Package ID** is `MMDVM_MMDVM_HS_Hat` and **Software ID** is `20200922`. A call on a talkgroup of the hotspot shows on the main tab of DroidStar, and in its log:

```text
New DMR stream from  3000001  to  91
New DMR stream from  3000001  to  3100
```

`hack/droidstar-test.sh` also restarts the hub 5 times with MSTCL and 5 times with MSTNAK. DroidStar logged in again each time, about 10 seconds after an MSTCL and 12 to 15 seconds after an MSTNAK.
