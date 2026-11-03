#!/bin/sh
# Usage: hack/carry/frontend.sh, from the top of the repository, after the
# frontend build and before go build.
#
# Adds to internal/http/frontend/dist/assets the assets of earlier releases
# that carry-assets.sh keeps, from frontend-assets.tar.zst of the release
# before this one, and writes artifacts/frontend-assets.tar.zst for this
# release. A release before the first one with this script has no such
# file, so the build after it is a first build.
set -eu

repo=${CARRY_REPO:-https://github.com/USA-RedDragon/DMRHub}
assets=internal/http/frontend/dist/assets

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/previous" "$work/new" artifacts

previous=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true)
if [ -n "$previous" ] && git cat-file -e "$previous:hack/carry/carry-assets.sh" 2>/dev/null; then
	curl -fsSL -o "$work/previous.tar.zst" "$repo/releases/download/$previous/frontend-assets.tar.zst"
	tar --zstd -x -f "$work/previous.tar.zst" -C "$work/previous"
else
	echo "frontend.sh: ${previous:-no earlier release} predates the carried assets" >&2
	CARRY_FIRST_BUILD=1
	export CARRY_FIRST_BUILD
fi

sh hack/carry/carry-assets.sh "$work/previous/ledger" "$work/new/ledger" "$work/previous/assets" "$assets"
tar --zstd -c -f artifacts/frontend-assets.tar.zst -C "$work/new" ledger -C "$PWD/internal/http/frontend/dist" assets
