# DMR Server in a box

Run a DMR network server with a single binary. Includes private and group calls and a Parrot. Perfect for quick deployments in emergency situations. Intended for use with AREDN.

## Current Status

This code was a 6-hour self-hackathon. Future revisions will help clean it up and separate concerns a bit better. But, group calls are functional (though all peers get all group calls) and private calls as well.

## External requirements

Right now, Redis is the only requirement

## Todos

- Add a Parrot
- Add a frontend (lastheard + self-service registration and repeater management)
- Add ability to link/unlink from talkgroups
- `vcsbk` call types
- document talkgroups in-app
