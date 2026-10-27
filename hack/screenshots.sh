#!/bin/bash
# Takes the screenshots of the docs: builds DMRHub, runs it on loopback
# with SQLite and demo data, and captures each page with headless Chromium
# at 1280x800, device scale 2, in the light theme.
#
# Environment:
#   DMRHUB_BIN          DMRHub binary to use (default: build one with
#                       go generate and go build, which needs npm and
#                       TinyGo 0.43.0 or later)
#   DMRHUB_SHOTS_WORK   work directory (default: artifacts/screenshots)
#   DMRHUB_SHOTS_OUT    directory of the screenshots (default: doc/Screenshots)
#   CHROMIUM            Chromium binary (default: /usr/bin/chromium)
#
# It needs unshare, pasta and Chromium, and oxipng to compress the PNG
# files losslessly.
#
# Each phase runs in a network namespace of its own. The setup phase gets
# only a loopback interface (unshare -r -n), as the setup wizard always
# listens on port 3005 of every address. The main phase runs under pasta,
# which gives it the internet for the tiles of the Positions map and
# forwards no ports. In both, Chromium sees none of the host's interfaces
# come and go: on a host with containers, each such change makes Chromium
# abort its requests with ERR_NETWORK_CHANGED, which leaves pages empty.
# The demo data goes in through the API and through hotspots that speak
# the MMDVM network protocol.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=${DMRHUB_SHOTS_WORK:-$root/artifacts/screenshots}
out=${DMRHUB_SHOTS_OUT:-$root/doc/Screenshots}
chromium=${CHROMIUM:-/usr/bin/chromium}

rm -rf "$work"
mkdir -p "$work/bin" "$out"
work=$(cd "$work" && pwd)
out=$(cd "$out" && pwd)

for opener in xdg-open x-www-browser www-browser; do
    printf '#!/bin/sh\nexit 0\n' > "$work/bin/$opener"
    chmod +x "$work/bin/$opener"
done

cd "$root"
if [ -n "${DMRHUB_BIN:-}" ]; then
    cp "$DMRHUB_BIN" "$work/DMRHub"
else
    go generate ./...
    go build -ldflags "-X main.version=$(git describe --tags --always) -X main.commit=$(git rev-parse --short HEAD)" -o "$work/DMRHub" .
fi
go build -o "$work/screenshots" ./hack/screenshots

run=("$work/screenshots" -hub "$work/DMRHub" -chromium "$chromium" -work "$work" -out "$out")
unshare -r -n sh -c 'ip link set lo up && exec "$@"' sh "${run[@]}" -phase setup
pasta --config-net --quiet -t none -u none -T none -U none -- "${run[@]}" -phase main

if command -v oxipng > /dev/null; then
    oxipng -q -o max --strip safe "$out"/*.png
else
    echo "oxipng not found; the screenshots are not compressed" >&2
fi
ls -l "$out"
