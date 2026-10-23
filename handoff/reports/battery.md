# battery (lane/battery) final report
- 17 commits. Fixes: parrot refresh stream before it can end (test failed 50/50 before); mmdvm stop socket reader on server stop (failed 5/5 before). Harness + areas + runner + graphs: `make stress` / `hack/stress.sh`; DMRHUB_STRESS_AREAS (callmix kinds transports ramp latency soak chaos hostile webclients database repro), DMRHUB_STRESS_BACKENDS (sqlite, sqlite-memory, postgres-redis), DMRHUB_STRESS_EXT_DIR for external clients (run in unshare -r -n). Results to artifacts/stress/<time> (JSON, CSV, logs, pprof, PNG).
- External: DMRGateway+MMDVMHost, HBlink3, HBlink4, MMDVM_Bridge logged in and heard 3/3; YSF2DMR login only.
- First pass (SQLite, machine load 20-74): every kind forwarded unchanged exactly once. Latency degrades at 5 busy repeaters (p99 217 ms), 10 repeaters 1.3 s; hub under half a core, waiting on DB pool (74 s waits in a 15 s step). 34-51 SQL statements per burst (load independent). Postgres collapsed at 8 repeaters (11% delivered, p50 26 s).
- Failures found (not fixed):
  1. DB per burst: FindRepeaterByID in subscribeTG dominates pool waits (block profile) -> pubsub drops, split rows, missing messages, SQLITE_BUSY leaving rows active. (lane/i9-perf cache addresses part; re-measure.)
  2. Each ping runs 13 statements incl. a Save upserting associations, on the single MMDVM ingress goroutine.
  3. Shutdown hang: UnregisterServer blocks behind deliverToServer when the peer channel is full (repro 3/3).
  4. Back-to-back SMS on one slot lose message rows or get the wrong kind even on an idle hub; spaced 4 s apart they are correct.
  5. API rate limit race: 0/120 concurrent requests from one address got 429 (GORM store count read/write without a transaction).
  6. Login flood: 5000 RPTLs blocked a real login > 60 s (DB lookup under server lock: inference).
  7. HBRP login never answered (lane/hbrp addresses).
  8. No clock injection point; clock jumps untested.
  9. Held traffic: under Postgres load a burst arrived 125 s late (violates never-hold rule).
- Left: quiet-machine reruns, soak, full ramp, chaos below saturation.
