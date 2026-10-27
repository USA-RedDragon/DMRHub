# YSF2DMR

Tested 2026-10-27 with YSF2DMR 20200605 (github.com/juribeparada/MMDVM_CM at 7328c9e), against DMRHub 569c6e70. Only the login to DMRHub was tested.

## What it is

YSF2DMR is a part of MMDVM_CM. It connects a System Fusion (YSF) hotspot or reflector to a DMR master. It logs in to DMRHub as a hotspot with the MMDVM network protocol, and converts the calls between YSF and DMR.

We did not test calls, because the run had no YSF side.

## What to set in DMRHub

Enroll the `Id` of `[DMR Network]` at `<siteurl>/repeaters/new`, such as `300000106`, and copy the password.

## Configuration

Start from the `YSF2DMR.ini` of MMDVM_CM. These are the keys of `[DMR Network]` that the run set:

```ini
[DMR Network]
Id=300000106
StartupDstId=91
StartupPC=0
Address=dmrhub.example.org
Port=62031
Jitter=500
EnableUnlink=1
TGUnlink=4000
PCUnlink=0
Password=<repeater password>
TGListFile=TGList-DMR.txt
Debug=0
```

`StartupDstId=91` with `StartupPC=0` starts on TG 91 as a group call. `TGUnlink=4000` matches the unlink talkgroup of DMRHub, 4000.

## Check that it works

The YSF2DMR log shows `DMR, Logged into the master successfully`. On `<siteurl>/repeaters/300000106`, **Package ID** is `MMDVM_DMO`, **Software ID** is `20200605`, and the description is `Multi-Mode Repeater`. In our run, YSF2DMR logged in about 10 seconds after it started.
