# DMRHub stress battery (release gate)

The owner's release gate: this battery must pass before the DMRHub release with the dmrgo v2.0.0 port and the transparent traffic, typed last heard and SMS inbox/outbox work ships. DMRHub stays v1.x; never call it "DMRHub v2".

Build it once so it can be rerun. Reuse lane E's helpers in internal/testutils: IntegrationStack, SpawnSecondReplica, RestartMMDVM, StartDroidStar, MMDVMClient.SendRaw, the OpenBridge client, the IPSC client and wsclient.go. Put long runs behind a build tag (`stress`) and/or env vars (duration, scale) so the normal `go test` stays fast. Loopback or network namespaces only. Never connect to a public master or network.

| Area | What to do | Pass criteria |
|---|---|---|
| Call mix | Many repeaters at once. Group and private calls on both slots. Back-to-back key-ups, late entry, lost terminators, 3-minute calls, 300 ms kerchunks, the parrot. | Every call is delivered once, to exactly the right targets, with correct last-heard rows. No stuck streams. |
| Every traffic kind | Voice, confirmed and unconfirmed data at rate 1/2, 3/4 and 1, SMS in every format, CSBK (radio check, call alert), MBC, UDT, USBD, talker alias, GPS, idle, and random or unknown bursts, mixed on the same slots. | Everything spec-valid is forwarded unchanged (PLAN section 3) and logged with the right kind. Messages are stored and visible only by the BRIEF rules. |
| Every transport | MMDVM, HBRP (not the same as MMDVM; see refs hbrp-vs-mmdvm.md), OpenBridge v1 and enhanced, and IPSC peers together. Real external clients where possible: MMDVMHost, DMRGateway, HBlink3/4, FreeDMR, xlxd, DroidStar (another lane is building them under /home/user/work/ext, see /home/user/work/ext/PINS.md when it exists). | The same results as the lane E matrix (internal/dmr/transparency_test.go and friends), under load. |
| Load ramp | Raise the number of repeaters and parallel calls until something degrades. | Record where it degrades and what the limit is (CPU, DB, Redis, a lock). Per-burst forwarding latency must stay low. Data must never queue behind voice. |
| Soak | Hours of steady mixed traffic. | Memory, goroutines, file descriptors and DB connection counts stay flat. No queue keeps growing. No error log spam. |
| Chaos | Peers dropping mid-call, master restarts, Redis or Postgres outage and failover, replica scale and failover, packet loss, reorder, duplicates, clock jumps. | Recovers without a restart. Nothing stalls. Lost traffic is counted, never silently held. |
| Hostile input | Malformed and oversized packets (lane A has `mmdvm: test truncated packets`), login floods, spoofed source IDs, attempts to fill the message store, cross-site websocket origins. | No panic or crash. No privacy leak. Rate limits hold. |
| Latency | Per-burst forwarding time under load. | About processing time. Owner: stalls are bad for everything, voice and data (TN3270-style data apps will run over this). |
| Slow web clients | Many open last-heard and messages pages; clients that stop reading. | Stalled clients are dropped by the per-write timeout. Publishers and other clients are never delayed. |
| Database | Message storage under load, the hourly retention purge during traffic, the 250 caps. | Correct results, no lock storms. |

Report each failure with its root cause and fix. Rerun until the whole battery passes. Then a short results doc with numbers: tables plus a PNG graph for numeric results.
