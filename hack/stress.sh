#!/bin/bash
# Runs the stress battery (internal/stress, build tag stress) area by area
# and writes the results and the graphs.
#
# Environment:
#   DMRHUB_STRESS_AREAS     areas to run, space separated (default: all)
#                           callmix kinds transports ramp latency soak chaos
#                           hostile webclients database repro
#   DMRHUB_STRESS_BACKENDS  backends, space separated (default: sqlite)
#                           sqlite (a SQLite file with the default parameters),
#                           sqlite-memory, postgres-redis, mysql, mariadb (Docker)
#   DMRHUB_STRESS_RESULTS   results directory
#                           (default: artifacts/stress/<UTC time>)
#   DMRHUB_STRESS_DURATION  traffic time of an area (default 20s; soak: set hours)
#   DMRHUB_STRESS_REPEATERS repeaters of the rig (default 20)
#   DMRHUB_STRESS_FANOUT    repeaters per talkgroup and slot (default 5)
#   DMRHUB_STRESS_DUTY      share of the time each slot transmits (default 0.6)
#   DMRHUB_STRESS_RAMP      busy repeaters of each ramp step (default 10,25,50,100)
#   DMRHUB_STRESS_STEP      time of a ramp step (default: the duration)
#   DMRHUB_STRESS_LONG_CALL length of the long call of the call mix
#                           (default: 80% of the duration, at most 3m)
#   DMRHUB_STRESS_P99_MS    p99 latency limit in ms (default 50)
#   DMRHUB_STRESS_WS_CLIENTS websockets of the webclients area (default 40)
#   DMRHUB_STRESS_BACKLOG   old rows of the database area (default 20000)
#   DMRHUB_STRESS_PPROF     set to write CPU, heap, mutex and block profiles
#   DMRHUB_STRESS_EXT_DIR   directory of the external clients (PINS.md); when set,
#                           the transports area runs them, inside unshare -r -n
#                           (so on a SQLite backend only)
#   DMRHUB_STRESS_EXT       external clients to run (default: all of
#                           dmrgateway hblink3 hblink4 mmdvm_bridge ysf2dmr)
#   DMRHUB_STRESS_TIMEOUT   go test timeout of an area (default 60m)
#   DMRHUB_STRESS_CLOCKSTEP set to 1 to run the chaos area on a SQLite backend
#                           in the throwaway VM of hack/clockstep.sh, where its
#                           clock-jump steps step the wall clock (needs
#                           qemu-system-x86_64 with KVM, cpio and zstd)
#   PYTHON                  python with matplotlib for the graphs
#                           (default /home/user/work/venv/bin/python)
set -u

cd "$(dirname "$0")/.."
root=$(pwd)
results=${DMRHUB_STRESS_RESULTS:-$root/artifacts/stress/$(date -u +%Y%m%dT%H%M%SZ)}
areas=${DMRHUB_STRESS_AREAS:-callmix kinds transports ramp latency soak chaos hostile webclients database repro}
backends=${DMRHUB_STRESS_BACKENDS:-sqlite}
timeout=${DMRHUB_STRESS_TIMEOUT:-60m}
python=${PYTHON:-/home/user/work/venv/bin/python}

declare -A tests=(
    [callmix]=TestCallMix [kinds]=TestEveryKind [transports]=TestTransports
    [ramp]=TestLoadRamp [latency]=TestLatency [soak]=TestSoak [chaos]=TestChaos
    [hostile]=TestHostileInput [webclients]=TestSlowWebClients [database]=TestDatabase
    [repro]='TestRepro.*'
)

mkdir -p "$results"
bin="$results/stress.test"
go test -tags stress,codec2wasm -c -o "$bin" ./internal/stress/ || exit 1

status=0
for backend in $backends; do
    dir="$results/$backend"
    mkdir -p "$dir"
    for area in $areas; do
        test=${tests[$area]:-}
        if [ -z "$test" ]; then
            echo "unknown area $area" >&2
            status=1
            continue
        fi
        echo "== $backend $area ($test)"
        start=$(date +%s)
        if [ "$area" = chaos ] && [ "${DMRHUB_STRESS_CLOCKSTEP:-}" = 1 ]; then
            if [ "$backend" = postgres-redis ]; then
                echo "chaos in the clock step VM needs a SQLite backend; skipped" | tee "$dir/$area.out"
                continue
            fi
            vmenv="DMRHUB_STRESS_BACKEND=$backend DMRHUB_STRESS_RESULTS=/out"
            for v in DURATION REPEATERS FANOUT DUTY CHAOS_BUSY P99_MS PPROF; do
                n=DMRHUB_STRESS_$v
                [ -n "${!n:-}" ] && vmenv="$vmenv $n=${!n}"
            done
            PACKAGES=./internal/stress TAGS=stress,codec2wasm RUN="^${test}\$" SMP=4 MEM=4G \
                ARGS="-test.count=1 -test.timeout $timeout" VMENV="$vmenv" OUT="$dir" \
                hack/clockstep.sh > "$dir/$area.out" 2>&1
            rc=$?
            echo "$backend,$area,$rc,$(( $(date +%s) - start ))" >> "$results/exit-codes.csv"
            grep -E '^(--- |FAIL|ok|PASS)' "$dir/$area.out" | head -20
            [ $rc -ne 0 ] && status=1
            continue
        fi
        wrap=()
        if [ "$area" = transports ] && [ -n "${DMRHUB_STRESS_EXT_DIR:-}" ] && [ "${backend#sqlite}" != "$backend" ]; then
            wrap=(unshare -r -n bash -c 'ip link set lo up && DMRHUB_STRESS_NETNS=1 exec "$@"' _)
        fi
        (cd internal/stress && DMRHUB_STRESS_BACKEND=$backend DMRHUB_STRESS_RESULTS=$dir \
            "${wrap[@]}" "$bin" -test.run "^${test}\$" -test.count=1 -test.v -test.timeout "$timeout") \
            > "$dir/$area.out" 2>&1
        rc=$?
        echo "$backend,$area,$rc,$(( $(date +%s) - start ))" >> "$results/exit-codes.csv"
        grep -E '^(--- |FAIL|ok|PASS)' "$dir/$area.out" | head -20
        [ $rc -ne 0 ] && status=1
    done
done
rm -f "$bin"
rm -rf "$results"/*/data

if [ -x "$python" ]; then
    "$python" hack/stress_plot.py "$results"
else
    echo "no $python: skipping the graphs (python3 -m venv /home/user/work/venv && /home/user/work/venv/bin/pip install matplotlib)"
fi
echo "results in $results"
exit $status
