#!/bin/bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage: hack/hblink3-test.sh HBLINK3_SRC PYTHON [test flags]

Runs TestExternalHBlink3 of internal/dmr: an HBlink3 server (bridge.py of
HBLINK3_SRC, run by PYTHON) links to a test hub over OpenBridge version 1.
Group calls, private calls and SMS must cross the link both ways, and the
hub must drop forged packets on the link.

PYTHON is a Python with the packages of HBlink3's requirements.txt. The
test runs in a network namespace with only loopback (unshare -r -n).

Test flags go to the test binary, for example -test.count=3. Scratch files
go to a new directory in $DMRHUB_TEST_TMP, by default ~/.cache/dmrhub-hblink3,
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
base=${DMRHUB_TEST_TMP:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-hblink3}
mkdir -p "$base"
tmp=$(mktemp -d -p "$base")
trap 'rm -rf "$tmp"' EXIT

cd "$root"
go test -tags codec2wasm -c -o "$tmp/dmr.test" ./internal/dmr/
cd internal/dmr
unshare -r -n env TMPDIR="$tmp" DMRHUB_HBLINK3="$src" DMRHUB_HBLINK3_PYTHON="$python" \
	bash -c 'ip link set lo up && exec "$0" -test.run "^TestExternalHBlink3$" -test.v "$@"' "$tmp/dmr.test" "$@"
