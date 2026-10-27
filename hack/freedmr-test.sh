#!/bin/bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage: hack/freedmr-test.sh FREEDMR_SRC PYTHON [test flags]

Runs TestExternalFreeDMR of internal/dmr: a FreeDMR server (bridge_master.py
of FREEDMR_SRC, run by PYTHON) links to a test hub over enhanced OpenBridge
version 5. Group calls and SMS must cross the link both ways, the hub must
drop forged packets on the link, and FreeDMR must keep sending to the hub
after a minute, which it does only while the hub sends BCKA keepalives.

PYTHON is a Python with the packages of FreeDMR's requirements.txt. FreeDMR
can fetch ID lists from public hosts, so the test runs in a network
namespace with only loopback (unshare -r -n).

Test flags go to the test binary, for example -test.count=3. Scratch files
go to a new directory in $DMRHUB_TEST_TMP, by default ~/.cache/dmrhub-freedmr,
which is removed at the end.
EOF
}

if [ $# -lt 2 ]; then
	usage >&2
	exit 2
fi
case "$1" in
-h | --help)
	usage
	exit 0
	;;
esac

src=$(realpath "$1")
python=$(realpath -s "$2")
shift 2
root=$(cd "$(dirname "$0")/.." && pwd)
base=${DMRHUB_TEST_TMP:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-freedmr}
mkdir -p "$base"
tmp=$(mktemp -d -p "$base")
trap 'rm -rf "$tmp"' EXIT

cd "$root"
go test -tags codec2wasm -c -o "$tmp/dmr.test" ./internal/dmr/
cd internal/dmr
unshare -r -n env TMPDIR="$tmp" DMRHUB_FREEDMR="$src" DMRHUB_FREEDMR_PYTHON="$python" \
	bash -c 'ip link set lo up && exec "$0" -test.run "^TestExternalFreeDMR$" -test.v "$@"' "$tmp/dmr.test" "$@"
