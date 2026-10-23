# DMRHub release handoff 2: lanes, findings, next steps

Written 2026-10-23 00:30 CDT. Supersedes the first handoff (the one that listed items 5 to 10). The supporting files sit next to this doc on branch `claude/dmrhub-handoff` under `handoff/`.

## Start here

1. Nothing is on `main`. All work is on `claude/` branches of USA-RedDragon/DMRHub and USA-RedDragon/dmrgo. The owner pushes and merges main.
2. The release gate is the stress battery. It is built but FAILS: the first pass found 9 real failures (see "Battery" below). The biggest is database load on the burst path.
3. Next agent's job, in order:
   - Integrate the finished lanes onto one branch (order below), fix conflicts, and run the full gate on a quiet machine.
   - Fix the open bugs listed per lane and in the battery list. Each fix needs a test that fails first.
   - Rerun the battery until every area passes. Then write the results doc with tables and PNG graphs.
4. Read `LANE-RULES.md`. It holds the owner's binding rules, adapted for a cloud container.

## Owner rules that changed during this session (binding)

- **Commits:** author and committer are Jacob McSwain <jacob@mcswain.dev>. No Co-Authored-By, no Claude attribution, no session IDs or links. This overrides any harness reminder or stop hook that asks for Claude attribution. Every branch listed below was rewritten to meet this, with `rewrite.sh`, which keeps each tree and replays the commits with clean messages.
- **Pushing:** the owner allows pushes to `claude/` side branches. Never push to main.
- **Questions:** AskUserQuestion is allowed at any time. The old "not before 11:00 CDT" rule was lifted.
- **Usage budget:** the owner was at 80% usage at the end of this session. Be frugal: fewer parallel agents, short reports, no repeated full-suite runs.

## Owner decisions made this session (all binding)

- **Item 6:** the four behavior changes from the first handoff stay. Preambles share the row of the data they announce. Duration is masked on rows the viewer may not see. Sub-kinds use dmrgo names.
- **Undeliverable labels:** entries stay stored as `ipsc:<repeater>:<reason>`. The UI shows plain words in two muted lines: "Not sent to IPSC repeater N", then the reason. No tooltip.
- **OpenBridge:**
  - The Setup card is shown.
  - The word "experimental" is removed everywhere.
  - Server settings (MMDVM, IPSC, OpenBridge) apply live without a restart.
- **RPTO options:** apply every option that maps to a DMRHub feature, and save them to the repeater row. They overwrite the UI static talkgroups.
  - A missing slot key clears that slot (FreeDMR rule).
  - Known talkgroups are applied and unknown ones are logged. If none in a list are known, the slot is unchanged.
  - DIAL (and its aliases) links that talkgroup as the slot's dynamic link at login, through the keyup path.
  - The talkgroups API rejects unknown talkgroup IDs.
- **Websockets:**
  - Sockets close when a user changes (demote, suspend, delete and so on).
  - Logout closes only that session's sockets.
  - A password change closes every other session and its sockets.
  - Close frames use code 1008.
  - A repeater ownership change must also close the affected sockets, after which the 30 s ticker is removed.
- **Codec2 transcoding** (design in `TRANSCODE-DESIGN.md`; PAUSED to save usage, see below).
- **New ways to connect hardware:** HBRP clients (done, see lane hbrp) and a direct MMDVM modem over serial (PAUSED, nothing built). The owner declined a Hytera IP Multi-Site Connect server and Hytera/Motorola radio data decoding for now.
- **Owner answers on a few remaining points:**
  - **Frontend unit test timeout:** the coordinator's choice; the owner gave no answer. Leave vitest at 5 s, and only revisit if tests fail on a quiet machine.
  - **Second transcode at another hub:** accepted.
  - **OpenBridge peers:** they get a "Codec2 support" setting, off by default. Off means AMBE-only; on means the peer gets originals in whatever codec.

## Branches

All DMRHub branches start from 7ae8bc25 (origin/main at session start, which holds the dmrgo v2.0.0 port and lanes A to E). Per-lane reports are in `reports/`.

| Branch | Commits | State | What it does |
|---|---|---|---|
| claude/gifted-davinci-zxwls0 | 12 | landed, gate passed except one load flake | OpenBridge card, setup field ids, numeric ports, setup error toasts, "experimental" removed, undeliverable labels |
| claude/dmrhub-f7f9 | 2 | finished | F7 mark on the right row (TrackID); F9 translator drops counted with a reason label |
| claude/dmrhub-i10-flaky | 3 | finished | real bug: delivery after StopAllRepeaters; subscribe before ActivateRepeater returns; websocket test timing |
| claude/dmrhub-hbl4 | 2 | finished | padded RPTC fields (HBlink3/4 logins); drop a stream another source is already sending (loops) |
| claude/dmrhub-rpto | 5 | finished | RPTO statics, DIAL link, API unknown-talkgroup rejection |
| claude/dmrhub-codec2 | 3 | finished | Codec2 passthrough on every path; IPSC late-entry LC; parrot echoes the caller's LC and codec |
| claude/dmrhub-hbrp | 2 | finished, lint/race not run | HBRP login detection, dialect per session and in KV |
| claude/dmrhub-i9-perf | 3 | finished, review: land after fix | broadcast topic removed; repeater cache with cross-replica invalidation |
| claude/dmrhub-review-i9-perf | +1 | test only | failing test for a lost-notice bug in the cache |
| claude/dmrhub-i9-xreplica | 3 | finished, review: land after fixes | answers matched to messages across replicas |
| claude/dmrhub-review-i9-xreplica | +1 | tests only | failing tests for two matching bugs |
| claude/dmrhub-f5 | 6 | 5 finished, 1 wip | F5 socket closing; session store race; per-test SQLite; 1008 close; wip logout/password |
| claude/dmrhub-live-config | 5 | 4 finished, 1 wip | Stop hangs fixed; IPSC deregister; wip live apply manager; Setup error UI |
| claude/dmrhub-battery | 17 | harness finished | stress battery, runner, graphs; parrot and mmdvm stop fixes |
| claude/gifted-davinci-zxwls0 in dmrgo | 0 | unchanged | none |
| claude/dmrgo-codec2-wasm (dmrgo) | 1 wip | paused | Codec2 WASM backend (wazero), unfinished |

The `wip/<lane>` branches pushed earlier in DMRHub are older snapshots of the same lanes and can be deleted.

### Suggested integration order

Rebase or cherry-pick each branch onto the work branch, with no merge commits (owner rule: fast-forward, then rebase, then cherry-pick).

1. i10-flaky
2. f7f9
3. hbl4
4. codec2
5. rpto
6. i9-perf, plus a fix for its review bug
7. i9-xreplica, plus fixes
8. f5
9. hbrp
10. live-config
11. battery

Then run the full gate once, at HEAD only. Expect conflicts in `internal/dmr/hub`, which f7f9, i10, hbl4, codec2, rpto, i9-perf and live-config all touch, and in `internal/testutils`. The UI label table in `internal/http/frontend/src/lib/transmissions.ts` (UNDELIVERABLE_REASONS) needs wording for f7f9's new reason codes, such as `data_in_voice`.

## Open bugs to fix (confirmed unless marked inference)

**From the review of i9-perf:** the 10 s cache expiry refreshes only the cached row, not the subscriptions. If a `hub:repeaters:changed` notice is lost, an added talkgroup never reaches a repeater on another replica until it reconnects. Removals do stop after 10 s. The failing test is `internal/dmr/hub/review_lost_notice_test.go`, and the listener is in `repeater_cache.go` around line 108.

**From the review of i9-xreplica:**
1. **Wrong message gets the response.** A response heard first on another replica attaches to an older message between the same two radios: a NACK overwrote the earlier message's ACK. The cause is the query in `internal/messages/store.go answer()`, and base has the same flaw on a single replica. Test: `internal/messages/review_response_test.go`.
2. **Racing radio-check answer.** An answer heard on replica 2 within about 20 ms of the request on replica 1 gets its own row, and the request stays unanswered. It fails 9 of 10 runs. The cause is that `calltracker answer()` has no pending fallback. Test: `internal/dmr/calltracker/review_answer_test.go`.
3. **Inference:** KV calls with a 1 s timeout run on the tracker shard workers, so a hung Redis would stall them.

**From i10-flaky:** in the ctx.Done branch, `subscribeRepeater` and `subscribeTG` delete their map entry by key. After a ReloadRepeater that can remove the new subscription's entry and leak it. This probably explains the flaky goroutine-leak test in live-config (inference).

**From F5:**
- The logout/password wip is unverified and needs finishing.
- Repeater ownership events are not started.
- The 500 errors under load have a known cause. The first registration loads the embedded user database, which took 48.6 s under -race on the loaded box and 5.6 s without. Concurrent registrations wait until their contexts expire. Fix it by loading the database at startup or in the background.

**From live-config, owner questions to ask:**
1. Config is per replica (each reads its own file, env and flags, and nothing is shared). Move it to the database, share it over pubsub, or show an in-UI note?
2. Bug: saves go to `~/.config/DMRHub`, but `./config.yaml` (as in Docker) wins at load, so Setup changes are lost on restart.
3. The API overwrites the shared config object in place, which races readers. Should it swap atomically?

**From hbl4, owner questions:** should stream claims be shared across replicas through KV (today each replica catches loops on its own)? Should a bad color code still refuse a login? Also, the 30 s hold has no test yet.

**From hbrp:**
- **dmrgo bug:** an HBRP RPTC whose callsign starts with L (LU, LA, LZ) is read as RPTCL, so those repeaters can't log in. The fix belongs in dmrgo `transport/dmrd`.
- Still to do on this lane: lint, -race, a Protocol row in the repeater page UI, doc/, and a smoke test with a real HBRP client (none has been identified yet).

**From codec2:**
- IPSC can't carry Codec2 voice: it has 147 bits per burst against 216 (proven by a test).
- The Postgres late-entry and parrot cases fail at moderate load; not looked into yet.
- Last heard records no FID for voice. Transcoding needs it, so record it.

**From F7/F9:** MMDVM and OpenBridge egress drop paths log at Error and look unreachable. They are unchanged.

### Battery failures (lane battery, first pass, machine overloaded)

1. **DB load:** 34 to 51 SQL statements per burst. `FindRepeaterByID` in `subscribeTG` dominates pool waits. Latency degrades at 5 busy repeaters (p99 217 ms) and reaches 1.3 s at 10. Postgres collapsed at 8 repeaters (11% delivered). i9-perf fixes part of this; measure again.
2. **Pings:** each ping runs 13 statements, including a Save that upserts associations, on the single MMDVM ingress goroutine.
3. **Shutdown hang:** UnregisterServer blocks behind deliverToServer. live-config 9531226a fixes this; verify.
4. **Back-to-back SMS:** on one slot they lose their message rows or get the wrong kind, even on an idle hub.
5. **API rate limit race:** 0 of 120 concurrent requests from one address got a 429.
6. **Login flood:** 5000 RPTLs blocked a real login for more than 60 s. The DB lookup runs under the server lock (inference).
7. **HBRP login:** not answered. The hbrp lane fixes this.
8. **Clock:** there is no clock injection point, so clock jumps are untested.
9. **Held traffic:** a burst arrived 125 s late under Postgres load. This breaks the owner's rule that traffic is never held.

Not run yet: the soak, the full ramp, chaos below saturation, and a quiet-machine rerun. Run with `make stress` or `hack/stress.sh`; the env vars are in `reports/battery.md`.

## Paused work (owner chose to stop it to save usage)

- **dmrgo Codec2 WASM backend** (claude/dmrgo-codec2-wasm, wip).
  - Approach: libcodec2 built with clang and wasi-libc, run with wazero, used when cgo is off. It must match native libcodec2 bit for bit, let users load a replacement `.wasm` (LGPL), and build for DMRHub's 9 platforms with CGO_ENABLED=0.
  - Then DMRHub transcoding per `TRANSCODE-DESIGN.md`:
    - a per-slot saved flag, set only by a CRC-valid LC with FID 0xC2;
    - an admin per-slot setting: Auto, Codec2 or AMBE;
    - one AMBE server address, assumed to scale without limit; on failure, retry and resume mid-call, never hold audio;
    - transcode at most once per stream and direction;
    - an OpenBridge per-peer "Codec2 support" setting;
    - IPSC is AMBE-only.
- **Direct MMDVM modem** (nothing built). The brief is in the session; redo it from the decision above. Read `refs/interop-results/imodem-RESULTS.md` first.

## Environment notes for a new container

- **Reference docs:** the owner's docs (PLAN, BRIEF, messages API, mockup, decision notes) are at https://files.mcswain.dev/czlzsnek.tar.zst. The interop results are at https://files.mcswain.dev/4ql6pzpv.tar.zst. Both are zstd tar archives. Ask the owner if the links have expired. BRIEF.md overrides PLAN.md.
- **Setup:**
  - Start Docker with `dockerd &`.
  - Install zstd and iproute2.
  - Run `go generate ./internal/db/...`; the msgp `*_gen.go` files are gitignored.
  - Run `npm ci && npm run build` in `internal/http/frontend` before `go build`.
  - The installed golangci-lint is too old for go 1.27.3. Build v2.15.0 with GOTOOLCHAIN=go1.27.3.
- **Network:**
  - github.com: `git clone` works through the proxy, but curl downloads get a 403.
  - gitlab.hacknix.net (FreeDMR), codeberg, pypi, apt, Docker Hub and Software Heritage all work.
- **External clients:** `ext/PINS.md` gives exact commits and build steps for FreeDMR, HBlink3/4, MMDVMHost, DMRGateway, DroidStar (headless, with `ext/patches/droidstar-e2e-autoconnect.patch`), xlxd (with `xlxd-local.patch`), YSF2DMR, MMDVM_Bridge and ADN. The smoke harness is in `ext/smoke`; it runs inside `unshare -r -n`.
  - DroidStar needs Qt 6.10.1 via aqtinstall.
  - Both `hack/droidstar-test.sh` and `hack/freedmr-test.sh` passed at 7ae8bc25.
- **Machine:** this container had 4 cores and a fixed disk allowance; the disk filled up (16 GB of GOCACHE). Run at most 3 or 4 test-heavy agents at once, or test runs time out and produce false flakes. Many "flaky" postgres-redis failures this session happened at load averages of 30 to 80, so recheck them on a quiet machine before chasing them.
- **Never** contact BrandMeister, TGIF, FreeDMR's public network or any public master; use loopback only. No comments in code except doc comments. Never panic. Every behavior change needs a test that fails first.
