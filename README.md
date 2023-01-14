# DMR Server in a box

Run a DMR network server with a single binary. Includes private and group calls and a Parrot. Perfect for quick deployments in emergency situations. Intended for use with AREDN.

## Current Status

This project is budding, deployment is expected in days. Future revisions will help clean it up and separate concerns a bit better. Everything is functional so far (excluding potential bugs) but some of the web interface needs finishing. The most major web components to register and get a repeater on the system are implemented, as well as to view the currently active talkgroups. The 9990 Parrot and 4000 Unlink are implemented.

## External requirements

Right now, Redis is the only requirement

## Using it in Pi-Star

Under Configuration -> Expert -> Full Edit: DMR GW, add the following to an unused `[DMR Network]` block:

```ini
[DMR Network 2]
Enabled=1
Address=FUTURE_MESH_ADDR
Port=62031
# Rewrite 8000001 -> 1
PCRewrite1=1,8009990,1,9990,1
PCRewrite2=2,8009990,2,9990,1
TypeRewrite1=1,8009990,1,9990
TypeRewrite2=2,8009990,2,9990
TGRewrite1=1,8000001,1,1,999999
TGRewrite2=2,8000001,2,1,999999
SrcRewrite1=1,9990,1,8009990,1
SrcRewrite2=2,9990,2,8009990,1
SrcRewrite3=1,1,1,8000001,999999
SrcRewrite4=2,1,2,8000001,999999
Password="SELFSERVICE_PASSWORD"
Debug=0
Id=YOUR_DMR_RADIOID
Location=1
Name=AREDN
```

## Todos

### Before first release

- CI
- CI build and release
- user needs to be able to map static talkgroups
- repeater password needs to be generated
- database configurable either postgres or sqlite
- Dockerize
- flags to env vars
- make cors hosts configurable
- redis auth

### Soon

- Track packets for lastheard
- details page for talkgroup with lastheard
- details page for repeater with lastheard
- details page for user with lastheard and repeaters
- users should be able to edit their name and callsign
- I should be able to update the dmrdb on the fly
- error handling needs to be double checked
- Fix MSTCL on master shutdown (signal trap)

### Long Term

- Implement API tests
- Implement UDP server tests
- metrics

## To test

- DMR sms

## Feature ideas

- Setting to use any free slot if possible. (i.e. timeslot routing where both slots are maximally active)
- Admin panels
  - see users where callsign and DMR ID don't match (due to dmr id db drift)
  - server configuration. Basically everything you'd see in env vars
- server allowlist
- server blocklist
- ability to lock down traffic on one timeslot to a list of designated users. Intended for ensuring a timeslot remains open for emergency use)
- channel allowlist (maybe useful?)
- channel blocklist (this seems rife for abuse in some communities. maybe make this configurable by server admin?)
