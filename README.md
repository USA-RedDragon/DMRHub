# DMR Server in a box

Run a DMR network server with a single binary. Includes private and group calls and a Parrot. Perfect for quick deployments in emergency situations. Intended for use with AREDN.

## Current Status

This code was a 6-hour self-hackathon. Future revisions will help clean it up and separate concerns a bit better. But, group calls are functional (though all peers get all group calls) and private calls as well.

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

- Add a frontend (lastheard + self-service registration and repeater management)
- Add ability to link/unlink from talkgroups
- Implement authentication
- Fix MSTCL on master shutdown (signal trap)
- Add logic to route repeaters with -01, -02, etc repeater suffixes
- Change callsign in parrot to parrot. Any other fields?

## To test

- DMR sms

## Feature ideas

- Setting to use any free slot if possible. (i.e. timeslot routing where both slots are maximally active)

## Relational ideas

A scratchboard for my database:

Users
User hasMany Repeaters
Repeater hasMany Talkgroups
Lastheard hasMany Users, Talkgroups, repeaters
