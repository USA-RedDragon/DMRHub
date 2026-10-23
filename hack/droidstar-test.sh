#!/bin/bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage: hack/droidstar-test.sh DROIDSTAR [test flags]

Runs TestExternalDroidStar of internal/dmr: a desktop DroidStar logs in to a
test hub as a hotspot, shows the call info of voice calls on both slots, and
logs in again after MSTCL and after MSTNAK, 5 times each, with the time of
each reconnect in the test log.

DROIDSTAR is the path of a nostar/DroidStar binary built with the
DROIDSTAR_E2E_AUTOCONNECT hook (it connects on start, with no vocoder and no
modem). DroidStar fetches host lists from public hosts when it starts, so the
test runs in a network namespace with only loopback (unshare -r -n).

Test flags go to the test binary, for example -test.count=3. Scratch files
go to a new directory in $DMRHUB_TEST_TMP, by default ~/.cache/dmrhub-droidstar,
which is removed at the end.
EOF
}

if [ $# -lt 1 ]; then
	usage >&2
	exit 2
fi
case "$1" in
-h | --help)
	usage
	exit 0
	;;
esac

bin=$(realpath "$1")
shift
root=$(cd "$(dirname "$0")/.." && pwd)
base=${DMRHUB_TEST_TMP:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-droidstar}
mkdir -p "$base"
tmp=$(mktemp -d -p "$base")
trap 'rm -rf "$tmp"' EXIT

cd "$root"
go test -c -o "$tmp/dmr.test" ./internal/dmr/
cd internal/dmr
unshare -r -n env TMPDIR="$tmp" DMRHUB_DROIDSTAR="$bin" \
	bash -c 'ip link set lo up && exec "$0" -test.run "^TestExternalDroidStar$" -test.v "$@"' "$tmp/dmr.test" "$@"
