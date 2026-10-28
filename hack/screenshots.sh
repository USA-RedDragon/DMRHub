#!/bin/bash
# Makes the screenshots of the docs from hack/screenshots/shots.yaml: builds
# DMRHub with a clock that the harness sets, runs it on loopback with SQLite
# and demo data, and captures each page with headless Chromium at 1280x800,
# device scale 2, in the light theme. With the same UI, a second run makes
# the same PNG files, byte for byte.
#
# Environment:
#   DMRHUB_SHOTS_WORK      work directory (default: artifacts/screenshots)
#   DMRHUB_SHOTS_OUT       directory of the screenshots (default: doc/Screenshots)
#   DMRHUB_SHOTS_ONLY      take only the shots whose names contain this text
#   DMRHUB_SHOTS_GENERATE  set to 0 to skip go generate (the frontend and
#                          the built-in plugins must be built already)
#   DMRHUB_SHOTS_WASM      plugin file for the plugin upload shots (default:
#                          build the parrot plugin with TinyGo)
#   DMRHUB_SHOTS_RADIO     directory of a built dmr-web (its dist directory)
#                          for the Browser Radio shots (default: clone
#                          dmr-web at the commit below and build it, which
#                          needs git, Go and npm)
#   DMRHUB_SHOTS_EXT       directory of the external programs that
#                          hack/external/setup.sh builds, for the DMRGateway
#                          shot (default: the default of setup.sh)
#   CHROMIUM               Chromium binary (default: /usr/bin/chromium)
#
# It needs Go, npm and TinyGo 0.43.0 or later, unshare, pasta, Chromium,
# and oxipng to compress the PNG files losslessly.
#
# The setup phase runs with only a loopback interface (unshare -r -n), as
# the setup wizard listens on port 3005 of every address. The main phase
# runs under pasta, which gives it the internet for the map tiles of the
# Positions page and forwards no ports. In both, Chromium sees none of the
# host's interfaces come and go, which would abort its requests with
# ERR_NETWORK_CHANGED.
set -euo pipefail

dmrweb_commit=c2fab6f75b9a7b205273f24ba5923ab0c11dbbfe

root=$(cd "$(dirname "$0")/.." && pwd)
work=${DMRHUB_SHOTS_WORK:-$root/artifacts/screenshots}
out=${DMRHUB_SHOTS_OUT:-$root/doc/Screenshots}
chromium=${CHROMIUM:-/usr/bin/chromium}
only=${DMRHUB_SHOTS_ONLY:-}

rm -rf "$work"
mkdir -p "$work/bin" "$out"
work=$(cd "$work" && pwd)
out=$(cd "$out" && pwd)

for opener in xdg-open x-www-browser www-browser; do
    printf '#!/bin/sh\nexit 0\n' > "$work/bin/$opener"
    chmod +x "$work/bin/$opener"
done

cd "$root"
if [ "${DMRHUB_SHOTS_GENERATE:-1}" != 0 ]; then
    go generate ./...
fi
go build -o "$work/screenshots" ./hack/screenshots
"$work/screenshots" -phase overlay -work "$work/clock-overlay"
CGO_ENABLED=0 go build -overlay "$work/clock-overlay/overlay.json" -o "$work/DMRHub" .
if [ -n "${DMRHUB_SHOTS_WASM:-}" ]; then
    cp "$DMRHUB_SHOTS_WASM" "$work/parrot.wasm"
else
    mkdir -p "$work/plugin-src"
    git ls-files -z | tar --null -T - -cf - | tar -xf - -C "$work/plugin-src"
    (cd "$work/plugin-src" && git init -q && git add -A &&
        GIT_AUTHOR_DATE=2026-01-01T00:00:00Z GIT_COMMITTER_DATE=2026-01-01T00:00:00Z \
        git -c user.name=demo -c user.email=demo@example.org -c commit.gpgsign=false commit -q -m demo &&
        git -c tag.gpgsign=false tag v1.0.0)
    (cd "$work/plugin-src/plugins/parrot/cmd/parrot" && tinygo build -target=wasip1 -no-debug -o "$work/parrot.wasm" .)
fi

radio=${DMRHUB_SHOTS_RADIO:-}
if [ -z "$radio" ]; then
    git init -q "$work/dmr-web"
    git -C "$work/dmr-web" fetch -q --depth 1 https://github.com/USA-RedDragon/dmr-web "$dmrweb_commit"
    git -C "$work/dmr-web" checkout -q FETCH_HEAD
    (cd "$work/dmr-web" && npm ci --no-audit --no-fund && npm run build)
    radio=$work/dmr-web/dist
fi

if [ -z "$only" ]; then
    find "$out" -name '*.png' -delete
fi
run=("$work/screenshots" -hub "$work/DMRHub" -chromium "$chromium" -work "$work" -out "$out" -list "$root/hack/screenshots/shots.yaml" -only "$only" -radio "$radio" -root "$root"
    -ext "${DMRHUB_SHOTS_EXT:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-ext}")
unshare -r -n sh -c 'ip link set lo up && exec "$@"' sh "${run[@]}" -phase setup
pasta --config-net --quiet -t none -u none -T none -U none -- "${run[@]}" -phase main

if command -v oxipng > /dev/null; then
    oxipng -q -o max --strip safe "$out"/*.png
else
    echo "oxipng not found; the screenshots are not compressed" >&2
fi
"$work/screenshots" -phase check -root "$root" -list "$root/hack/screenshots/shots.yaml"
