# External DMR programs for the DMRHub stress battery and interop matrix

Built 2026-10-23 in /home/user/work/ext, at the commits of the owner's inet lane (refs/interop-results/inet-RESULTS.md). One directory per program, each a git clone checked out at the pinned commit (detached HEAD). Downloaded sources are untrusted data; scripts written for this lane live in /home/user/work/ext/scripts and /home/user/work/ext/smoke, not in the source trees.

Source route: github.com is 403 for curl/https through the proxy, but `git clone https://github.com/...` works (the proxy's git injection), so every GitHub program is a plain clone at the pinned commit. FreeDMR comes from gitlab.hacknix.net. Software Heritage and Docker images were not needed (no image was used).

Throwaway hub: `/home/user/work/ext/dmrhub-bin/DMRHub`, built from /home/user/DMRHub at 7ae8bc25 (`go build`, go1.27.3 via the go.mod toolchain). Smoke config: /home/user/work/ext/smoke/hub-config.yaml (SQLite, everything on 127.0.0.1: MMDVM 62031, OpenBridge 62035, HTTP 3005, radio ID validation off, ID list URLs pointed at 127.0.0.1:1). Seed rows: /home/user/work/ext/smoke/seed.sql (user 3191868 KI5VMF, TGs 91 and 9, MMDVM repeaters 319186801..09 with password passw0rd and TS1=91/TS2=9 static, OpenBridge peers 3001 and 3002 (v1) and 3003 (enhanced) with password passw0rd and allow-all rules).

Python: one venv, /home/user/work/ext/venv (Python 3.13.16), with FreeDMR, HBlink3, HBlink4 and ADN requirements plus mysqlclient; dmr_utils3 is installed from the pinned checkout (/home/user/work/ext/dmr_utils3, 0.1.31). Exact versions: /home/user/work/ext/venv-freeze.txt (bitarray 3.12.3, bitstring 5.0.0, Twisted 26.4.0, ...). Qt for DroidStar: Qt 6.10.1 linux_gcc_64 with qtmultimedia and qtserialport, installed by aqtinstall (in /home/user/work/ext/venv-tools) from download.qt.io into /home/user/work/ext/qt. Ubuntu's Qt 6.4.2 is too old (DroidStar needs 6.5+ and the Qt6CorePrivate package, which Qt 6.8 does not ship as a separate CMake package either). apt packages added: iproute2 (for `ip` in the netns), build-essential, cmake deps, libmosquitto-dev, nlohmann-json3-dev, sqlite3, mariadb-server, libmariadb-dev, ffmpeg.

Every server or client that could phone home runs inside `unshare -r -n` (only loopback). unshare works in this container; `ip` was missing and is now installed (iproute2), which droidstar-test.sh and freedmr-test.sh need.

## Table

| Program | Full commit | Source URL | Build | Binary / entry point | Smoke vs DMRHub |
|---|---|---|---|---|---|
| FreeDMR | 546165eb9a53e8c1616a71f74a5858a122c805af | https://gitlab.hacknix.net/hacknix/FreeDMR.git | none (Python, venv) | `/home/user/work/ext/venv/bin/python -E -s /home/user/work/ext/freedmr/bridge_master.py -c CFG` | hack/freedmr-test.sh PASS |
| HBlink3 | 1db27305c29024a47f6c2243cb44c5df47198c13 | https://github.com/HBLink-org/hblink3.git | none (Python, venv) | `venv/bin/python -E -s /home/user/work/ext/hblink3/bridge.py -c /abs/hblink.cfg -r /abs/rules.py` | PASS: PEER login + RPTO, TG91 call received on PEER and on OpenBridge v1 |
| HBlink4 | 2348c3bcc5cbee4e945652ffbdb0bc722a403425 | https://github.com/n0mjs710/hblink4.git | none (Python, venv) | `cd /home/user/work/ext/hblink4 && venv/bin/python -E -s run_hblink.py /abs/config.json` | PASS with string coordinates (see finding 1); outbound gets TG91, OpenBridge gets TG9 |
| MMDVMHost | 9384d683c9993f67af52b062b1bf9d4df11b6446 | https://github.com/g4klx/MMDVMHost.git | `make -j3` in tree | `/home/user/work/ext/mmdvmhost/MMDVM-Host INI` (binary is now named MMDVM-Host) | PASS (null modem, behind DMRGateway): net call TG91 decoded, BER 0.0% |
| DMRGateway | d751e20ed70816b3a834d3521aea58f03dd0b398 | https://github.com/g4klx/DMRGateway.git | `make -j3` in tree | `/home/user/work/ext/dmrgateway/DMRGateway INI` | PASS: login, RPTO, rule trace, call passed to MMDVMHost |
| DroidStar (headless) | becf1dede57c4a70a865e9cb4cac9247e505599a + patches/droidstar-e2e-autoconnect.patch | https://github.com/nostar/DroidStar.git | `cmake -S droidstar -B droidstar-build -DCMAKE_BUILD_TYPE=Release -DCMAKE_PREFIX_PATH=/home/user/work/ext/qt/6.10.1/gcc_64 && cmake --build droidstar-build -j3` | `/home/user/work/ext/droidstar-build/DroidStar` | hack/droidstar-test.sh PASS |
| xlxd | bc6f96fdcd97d857ef801ad6667186a626e38bcd + patches/xlxd-local.patch | https://github.com/LX3JL/xlxd.git | `make -j3` in xlxd/src | `/home/user/work/ext/xlxd/src/xlxd XLX000 127.0.0.1 127.0.0.1` | n/a vs DMRHub (xlxd is a master, DMRHub has no outbound HBRP); binary checked with an HBRP client: login, link module A, stream opened |
| YSF2DMR (MMDVM_CM) | 7328c9e9532d29cf4d9d7d24541d57f825207be4 | https://github.com/juribeparada/MMDVM_CM.git | `make -j3` in MMDVM_CM/YSF2DMR | `/home/user/work/ext/mmdvm_cm/YSF2DMR/YSF2DMR INI` | PASS: login (RPTL/RPTK/RPTC) |
| MMDVM_Bridge | 285e51e98dca637eab296a8fe00879e39381f678 (closed binary 20210520_V1.6.8, sha256 afc4c7f30ac3376afb195b71facfab102fcda5c7b3d0b5e03e42cfad69e7b380) | https://github.com/DVSwitch/MMDVM_Bridge.git | none (prebuilt amd64 binary in the repo, same version as the owner's) | `/home/user/work/ext/mmdvm_bridge/opt/MMDVM_Bridge/MMDVM_Bridge.amd64 INI` (DVSwitch.ini read from the cwd) | PASS: login + RPTO, TS2 call received |
| ADN-DMR-Peer-Server | 2198dfad4e15a37eb207b0505322c31160cddf1b | https://github.com/Amateur-Digital-Network/ADN-DMR-Peer-Server.git | none (Python, venv + mysqlclient; MariaDB 10.11 from apt run per test) | `cd /home/user/work/ext/adn && venv/bin/python -E -s adn-server.py -c /abs/adn-server.yaml` | PASS: enhanced OpenBridge v5 both ways, 8/8 packets each way |
| dmr_utils3 | 079d3e4a9d8903db589be23262c28c191bc9217d (0.1.31) | https://github.com/n0mjs710/dmr_utils3.git | `pip install --no-deps ./dmr_utils3` into the venv | library | used by FreeDMR/HBlink3/HBlink4 above |

Not built: urfd (inet lane: source review only), DVMHost/FNE (not compatible), DMR2YSF/DMR2NXDN (not run by the owner either). MMDVM_CM's other gateways are in the same checkout and build the same way if needed.

## Patches (minimal, applied in the working trees, saved in /home/user/work/ext/patches)

droidstar-e2e-autoconnect.patch, against becf1de. The MSTCL/MSTNAK reconnect fix 0580c7d ("fix(dmr): MSTCL/MSTNAK reconnect never fires", Jacob McSwain, 2026-02-12) is already upstream and is an ancestor of becf1de (`git merge-base --is-ancestor 0580c7d becf1de` is true), so it needs no patch. The patch does two things:

- CMakeLists.txt: `if(TRUE) # set TRUE for md380_vocoder` becomes `if(FALSE)`, so no md380 vocoder (no USE_MD380_VOCODER, no libmd380_vocoder link).
- droidstar.cpp, end of the DroidStar constructor: when env DROIDSTAR_E2E_AUTOCONNECT is set, a single-shot QTimer after that many ms sets m_vocoder and m_modem to "None" and calls process_connect() if disconnected. That is what internal/testutils/droidstar.go needs: it writes ~/.config/dudetronics/droidstar.ini (MODE=DMR, DMRHOST=DMRHUB, VOCODER=None, MODEM=None) and DMRHosts.txt, starts the binary with QT_QPA_PLATFORM=offscreen, QT_LOGGING_RULES=*.debug=true and DROIDSTAR_E2E_AUTOCONNECT=1000, and waits for DroidStar's own qDebug lines ("New DMR stream from  X  to  Y", "Received MSTCL from master", "Received MSTNAK from master").

xlxd-local.patch, against bc6f96f, src/main.h only: no daemon (RUN_AS_DAEMON off), DEBUG_NO_G3_ICMP_SOCKET on, DMR ID and YSF node DBs read from local files instead of the RLX server (xlxapi.rlx.lu), and the /xlxd/* and /var/log/xlxd.xml paths moved to /home/user/work/ext/xlxd-run. xlxd needs the client's DMR ID in xlxd-run/dmrid.dat (`3191868;KI5VMF;Name`); it accepts IDs of at most 8 digits (cdmriddir.cpp:150), so a 9-digit hotspot ID such as 319186809 is "unrecognized id".

## How to run each against a DMRHub master on 127.0.0.1

The smoke driver does all of this: `bash /home/user/work/ext/smoke/smoke.sh NAME` with NAME one of dmrgateway, ysf2dmr, mmdvm_bridge, hblink3, hblink4, adn, xlxd. It re-executes itself under `unshare -r -n`, starts DMRHub from template.db + seed.sql, starts the program with the config shown in the script, sends a replayed call with scripts/hbrp_call.py (stdlib HBRP client that replays the 8 DMRD frames of DMRHub's internal/dmr/testdata/dmrgateway-master.txt with its own repeater and stream IDs), prints the program's log, the repeaters DMRHub has as connected, and DMRHub's call log, then kills everything. Work dirs: /home/user/work/ext/run/smoke-NAME. The full configs are in smoke.sh; the parts that matter:

MMDVMHost + DMRGateway (MMDVMHost has no direct master network any more; it only talks to DMRGateway):
```
# MMDVM-Host.ini
[General]  Id=3191868
[Modem]    Protocol=null
[DMR]      Enable=1
[DMR Network]  Enable=1  LocalAddress=127.0.0.1 LocalPort=62042 GatewayAddress=127.0.0.1 GatewayPort=62041
[Log] MQTTLevel=0 DisplayLevel=1   (also disable CW Id, POCSAG, FM, FM Network)
# DMRGateway.ini
[General]  Id=319186801 RptAddress=127.0.0.1 RptPort=62042 LocalAddress=127.0.0.1 LocalPort=62041 RuleTrace=1
[Voice] Enabled=0   [XLX Network] Enabled=0
[DMR Network 1]  Enabled=1 Name=DMRHub Address=127.0.0.1 Port=62031 Password=passw0rd Options=TS1=91;TS2=9;
                 PassAllTG=1 PassAllTG=2 PassAllPC=1 PassAllPC=2
```
Note: DMRGateway's default LocalPort is 62031, the same as DMRHub's MMDVM port; move one of them.

YSF2DMR: YSF2DMR.ini `[DMR Network] Id=319186802 Address=127.0.0.1 Port=62031 Password=passw0rd StartupDstId=91 StartupPC=0`, absolute paths for DMRIds.dat and TGList-DMR.txt, `[Log] FileLevel=0`.

MMDVM_Bridge: MMDVM_Bridge.ini `[General] Id=319186803`, `[DMR] Enable=1`, `[DMR Network] Enable=1 Address=127.0.0.1 Port=62031 Local=62043 Password=passw0rd Options=TS1=91;TS2=9;`, log FilePath and DMR Id File moved off /var; DVSwitch.ini in the cwd. It only passes the DVSwitch export slot ([DMR] slot = 2 in DVSwitch.ini): a TS1 call from DMRHub reaches it on the wire but it logs nothing.

HBlink3 (hblink.cfg, run with bridge.py and a rules.py; hblink.py chdirs to its own directory, so config, rules and ALIASES/GLOBAL PATH must be absolute, and the alias JSON files must be non-empty dicts such as `{"rptrs": []}`, otherwise dmr_utils3 mk_id_dict raises IndexError):
```
[PEER-DMRHUB]  MODE: PEER  IP: 127.0.0.1 PORT: 62053 MASTER_IP: 127.0.0.1 MASTER_PORT: 62031
               PASSPHRASE: passw0rd RADIO_ID: 319186804 OPTIONS: TS1=91;TS2=9;
[OBP-DMRHUB]   MODE: OPENBRIDGE IP: 127.0.0.1 PORT: 62051 NETWORK_ID: 3001 PASSPHRASE: passw0rd
               TARGET_IP: 127.0.0.1 TARGET_PORT: 62035 BOTH_SLOTS: True
```
DMRHub side: peers row id 3001, ip 127.0.0.1, port 62051, password passw0rd, ingress/egress 1, enhanced 0, plus peer_rules both directions.

HBlink4 (config.json):
```
"global": {"bind_ipv4": "127.0.0.1", "port_ipv4": 62061, "disable_ipv6": true, ...},
"dashboard": {"enabled": false},
"outbound_connections": [{"enabled": true, "address": "127.0.0.1", "port": 62031, "passphrase": "passw0rd",
   "radio_id": 319186805, "latitude": "30.26720", "longitude": "-97.74310", "options": "TS1=91", ...}],
"openbridge_connections": [{"enabled": true, "network_id": 3002, "local_address": "127.0.0.1", "local_port": 62052,
   "target_address": "127.0.0.1", "target_port": 62035, "passphrase": "passw0rd", "talkgroup_slots": {"9": "2"}}]
```
Keep the TGs of the outbound and the OpenBridge apart: with TG91 on both, HBlink4 forwards the hub's OpenBridge copy back up its outbound link and DMRHub logs the same stream ID again, many times, as short calls from repeater 319186805 with 50-70% loss (seen in a run; topology loop, not a crash).

FreeDMR: as in internal/dmr/freedmr_test.go (OPENBRIDGE system with ENHANCED_OBP True, PROTO_VER 5, NETWORK_ID equal to GLOBAL SERVER_ID, 4-digit, TARGET_PORT the hub's OpenBridge port; DMRHub peer row with that ID and enhanced=1). Run: `bash hack/freedmr-test.sh /home/user/work/ext/freedmr /home/user/work/ext/venv/bin/python`.

ADN: config generated by scripts/adn_config.py from adn-server.example.yaml: GLOBAL SERVER_ID 3003, VALIDATE_SERVER_IDS false, PROXY listen 127.0.0.1:62071, OBP_PROXY off, SELF_SERVICE off, SYSTEMS = SYSTEM (MASTER, passphrase passw0rd, TS1_STATIC 91) and OBP-DMRHUB (OPENBRIDGE, 127.0.0.1:62053, NETWORK_ID 3003, TARGET 127.0.0.1:62035, ENHANCED_OBP true, PROTO_VER 5). ADN refuses to start without MariaDB: smoke.sh runs mariadb-install-db + mariadbd on 127.0.0.1:3306 inside the same netns, with data in the run dir, and must delete the anonymous users (otherwise "Access denied for user 'adn'@'localhost'"). DMRHub side: peer 3003, enhanced=1.

DroidStar: `bash hack/droidstar-test.sh /home/user/work/ext/droidstar-build/DroidStar` (the test writes the settings; see internal/testutils/droidstar.go).

xlxd: `xlxd XLX000 127.0.0.1 127.0.0.1` listens for MMDVM/HBRP clients on 62030; DMRHub cannot connect to it (no outbound HBRP), so it is only usable as a second master for clients in the battery.

Env for the two hack scripts (they compile the test binary from /home/user/DMRHub into a temp dir; nothing is written to the checkout): `GOCACHE=/home/user/work/gocache GOTMPDIR=/home/user/work/gotmp DMRHUB_TEST_TMP=/home/user/work/ext/run`.

## Smoke results (2026-10-23)

- hack/droidstar-test.sh: PASS (135.7 s). Login 1.2 s; voice on TS1/TG91 and TS2/TG9 shown; reconnect after MSTCL 10.00/9.99/10.04/9.95/10.00 s; after MSTNAK 11.5/15.0/14.9/15.0/15.1 s; handoff keeps the session, first call heard after 0.43-0.53 s, no relogin. Log: /home/user/work/ext/run/droidstar-test.log.
- hack/freedmr-test.sh: PASS (104.8 s), hub to FreeDMR, FreeDMR to hub, and again after a minute of keepalives. Log: /home/user/work/ext/run/freedmr-test.log.
- dmrgateway (MMDVMHost + DMRGateway): PASS. "DMRHub, Logged into the master successfully" (with options), DMRHub "Repeater connected repeaterID=319186801"; replayed call to TG91 reached MMDVMHost: "received network voice header from 3191868 to TG 91 ... 0% packet loss, BER: 0.0%".
- ysf2dmr: PASS, login as 319186802.
- mmdvm_bridge: PASS, login with options as 319186803; a TS2 call reached it ("received network voice header ... end of voice transmission, 0% packet loss"). Its "DMRD header doesn't match the DMR RF header" warning comes from the smoke client, which rewrites the DMRD destination to TG9 but not the LC inside the bursts.
- hblink3: PASS, PEER logged in with "Repeater Options Accepted"; one hub call to TG91 arrived on both the PEER ("*GROUP CALL START* ... PEER: 319186804 ... TGID 91") and the OpenBridge ("... OBP STREAM ID ... PEER: 3001 ... TGID 91"), 0.42 s each.
- hblink4: PASS with string coordinates: outbound logged in, "RX stream started TS/TGID: 1/91 ... from remote repeater 319186805", and OpenBridge "RX stream start src=3191868 tgid=9 -> TS2 ... packets=8". FAIL with numeric coordinates (finding 1).
- adn: PASS, ADN to hub 8/8 DMRD (DMRHub "Transmission ended ... repeater=3003"), hub to ADN 8/8 DMRD at a client logged in to ADN.
- xlxd: binary works (HBRP client login, "linking on module A", stream opened and closed); not applicable as a peer of DMRHub.

## Findings

1. Confirmed by observation, for the owner (DMRHub, not changed in this lane): DMRHub 7ae8bc25 refuses HBlink4 2348c3b's login. HBlink4 writes the RPTC latitude and longitude right-justified with spaces (hblink4/utils.py:326 pad_decimal, `str(value).encode()[:width].rjust(width)`, labelled "%8.8s"), so 30.2672 goes out as " 30.2672". DMRHub internal/db/models/repeater_configuration.go:120 runs strconv.ParseFloat on the raw field: `ERR Error parsing latitude error="strconv.ParseFloat: parsing \" 30.2672\": invalid syntax"`, `Invalid RPTC packet`, then MSTNAK, and HBlink4 retries every 5 s forever. Any latitude shorter than 8 characters or longitude shorter than 9 triggers it; with "30.26720"/"-97.74310" it logs in. The inet lane had HBlink4 outbound -> dmrgo master pass, so (inference, not checked) dmrgo trims or tolerates the padding. Reproduce: `HBLINK4_LAT=30.2672 bash /home/user/work/ext/smoke/smoke.sh hblink4`.
2. Observation (topology, inference about cause): with the same TG on an HBlink4 outbound and its OpenBridge link, the call loops back into DMRHub under the same stream ID from a different repeater, and DMRHub records it again many times. The battery should avoid that layout, or test it deliberately if DMRHub is meant to suppress a stream ID it is already carrying.
3. Container: `ip` was missing (both hack scripts call `ip link set lo up`); installed iproute2. unshare -r -n works.

## Layout

- /home/user/work/ext/<program>: source at the pinned commit (in-tree build products for the C++ programs).
- /home/user/work/ext/droidstar-build: DroidStar build dir.
- /home/user/work/ext/qt: Qt 6.10.1. /home/user/work/ext/venv: Python for the Python servers. /home/user/work/ext/venv-tools: aqtinstall only.
- /home/user/work/ext/patches: the two patches. /home/user/work/ext/scripts: swh_resolve.py (unused in the end), hbrp_call.py, adn_config.py.
- /home/user/work/ext/smoke: smoke.sh, hub-config.yaml, template.db (migrated empty DMRHub DB), seed.sql, the replay capture.
- /home/user/work/ext/logs: build and pip logs. /home/user/work/ext/run: test logs and smoke work dirs.
