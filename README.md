# DMRHub

[![Docker](https://github.com/USA-RedDragon/DMRHub/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/USA-RedDragon/DMRHub/actions/workflows/docker.yaml) [![Native](https://github.com/USA-RedDragon/DMRHub/actions/workflows/release.yaml/badge.svg)](https://github.com/USA-RedDragon/DMRHub/actions/workflows/release.yaml) [![go.mod version](https://img.shields.io/github/go-mod/go-version/USA-RedDragon/DMRHub.svg)](https://github.com/USA-RedDragon/DMRHub) [![GoReportCard example](https://goreportcard.com/badge/github.com/USA-RedDragon/DMRHub)](https://goreportcard.com/report/github.com/USA-RedDragon/DMRHub) [![License](https://badgen.net/github/license/USA-RedDragon/DMRHub)](https://github.com/USA-RedDragon/DMRHub/blob/master/LICENSE.md) [![Release](https://img.shields.io/github/release/USA-RedDragon/DMRHub.svg)](https://GitHub.com/USA-RedDragon/DMRHub/releases/) [![Downloads](https://img.shields.io/github/downloads/USA-RedDragon/DMRHub/total.svg)](https://GitHub.com/USA-RedDragon/DMRHub/releases/) [![GitHub contributors](https://badgen.net/github/contributors/USA-RedDragon/DMRHub)](https://GitHub.com/USA-RedDragon/DMRHub/graphs/contributors/)

Run a DMR network (like TGIF or BrandMeister) server with a single binary compatible with MMDVM. Includes private and group calls and a Parrot. Perfect for quick deployments in emergency situations. Intended for use with AREDN. 9990 Parrot and 4000 Unlink are implemented

## Quick links

- [FAQs](https://github.com/USA-RedDragon/DMRHub/wiki/FAQ)
- [Deployment Guide](https://github.com/USA-RedDragon/DMRHub/wiki/Deployment-Guide)
- [Admin's Guide](https://github.com/USA-RedDragon/DMRHub/wiki/Admin's-Guide)
- [User's Guide](https://github.com/USA-RedDragon/DMRHub/wiki/User's-Guide)

## Screenshots

![Lastheard](/doc/Screenshots/lastheard.png)

<details>
  <summary>More? Click to expand</summary>

![Repeaters](doc/Screenshots/repeaters.png)

![Easy Repeater Enrollment](doc/Screenshots/repeaters-easy.png)

![Repeater Management](doc/Screenshots/repeaters-edit.png)

![Talkgroup List](doc/Screenshots/talkgroups.png)

![Talkgroup Ownership](doc/Screenshots/talkgroup-ownership.png)

![User Approval](doc/Screenshots/user-approval.png)
</details>

## TODOs

### Soon

- Make talker alias do something
- details page for talkgroup with lastheard
- details page for repeater with lastheard
- details page for user with lastheard and repeaters
- users should be able to edit their name and callsign
- Fix MSTCL on master shutdown (signal trap)
- Hoseline equivalent

### Long Term

- Implement API tests
- Implement UDP server tests

## To test

- DMR sms and data packets

## Feature ideas

- Setting to use any free slot if possible. (i.e. timeslot routing where both slots are maximally active)
- Admin panels
  - see users where callsign and DMR ID don't match (due to dmr id db drift)
- server allowlist
- server blocklist
- ability to lock down traffic on one timeslot to a list of designated users. Intended for ensuring a timeslot remains open for emergency use)
- channel allowlist (maybe useful?)
- channel blocklist (this seems rife for abuse in some communities. maybe make this configurable by server admin?)
- add the ability for a talkgroup owner to create nets
- add the ability for talkgroup owner or net control operator to start/stop a net check-in
- add the ability for talkgroup owner or net control operator to see and export a check-in list (just query calls DB for TG=tg_id during net check-in period)
- distributed database? Maybe OLSR can help with the "where do I point my pi-star" problem that isn't a SPOF?
