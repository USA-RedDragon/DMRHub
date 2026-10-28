# Replicas and scaling

One DMRHub process can serve a whole network. To share the load or to restart without a gap, run several replicas of DMRHub behind one address. This page tells what the replicas need, how they share the work, and how to restart them one at a time.

## What several replicas need

- **Redis.** Turn on `redis.enabled` and set `redis.host` on every replica. The replicas pass the bursts to each other through Redis, and share the state of the network in it. Without Redis, each replica keeps this in its own memory and works alone.
- **PostgreSQL or MySQL.** Every replica must use the same database. A SQLite file belongs to one host, so use SQLite only with one replica.
- **The same configuration** on every replica, in particular the same `secret` and `password-salt`, so that a session from one replica works on the others.
- **One address for the network.** Put a UDP load balancer in front of the MMDVM, IPSC and WebTransport ports, and an HTTP load balancer in front of the web port.

When several replicas start at the same time, one of them migrates the database while the others wait. DMRHub takes a lock in the database for this: an advisory lock on PostgreSQL, and `GET_LOCK` on MySQL.

## Work that one replica does

Some work must happen in one place. A replica holds a lease in Redis for each of these, and another replica takes over when it stops:

| Work | Details |
| ---- | ------- |
| OpenBridge traffic | One replica sends all OpenBridge traffic, so that the peers see one source address. See [OpenBridge peers](openbridge.md#several-replicas). |
| Plugins inside DMRHub | One replica runs each plugin. See [Plugins](plugins.md#several-replicas). |
| APRS-IS | One replica logs in to APRS-IS. |
| Retention | One replica deletes the old records in each hour. |

Turn on the [MMDVM modem](modem.md) in one replica only.

## Stop a replica

When a replica gets SIGTERM (or SIGINT, SIGQUIT or SIGHUP), it does these steps:

1. `GET /api/v1/healthcheck` answers `503` with `{"status":"not ready"}`, so that the load balancer stops sending new users to the replica. Use this address as the readiness check of the replica.
2. When other replicas run, the replica hands its repeaters, its OpenBridge peers, its browser radios and its plugins over to the other replicas. It does this for at most `dmr.drain-timeout` seconds (default 25). 0 stops at once.
3. It waits for the calls that are in progress to end. This wait has no time limit.
4. It stops its servers and exits.

Set `dmr.drain-timeout` shorter than the time that your orchestrator gives the replica to stop. In Kubernetes, the default `terminationGracePeriodSeconds` is 30 seconds, which suits the default drain timeout of 25 seconds.

## Rolling restarts

Restart one replica at a time, and wait until `/api/v1/healthcheck` answers `200` with `{"status":"healthy"}` before you stop the next one.

With several replicas, a repeater that moves to another replica in a rolling restart keeps its login. The load balancer can drop what the old replica sent just before the move, so the old replica hands that over and the new replica sends it again. For an IPSC peer or a browser hotspot, this is what went out in the last 500 ms, and the peer drops what it already got. MMDVMHost and DMRGateway drop a repeated frame only when it repeats the last frame, and play other repeated frames as audio. So for an MMDVM repeater this is only the last frame on each timeslot that went out in the last 60 ms. When the load balancer takes more than 60 ms to pass on the packet that moves an MMDVM repeater, the repeater loses the bursts that the old replica sent before that. After a stall, frames can go out closer together than 60 ms, and the repeater can lose all but the last of them.
