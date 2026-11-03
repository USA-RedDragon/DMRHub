#!/bin/sh
# Usage: carry-assets.sh PREVIOUS_LEDGER NEW_LEDGER PREVIOUS_DIR NEW_DIR [PREVIOUS_DIR NEW_DIR]...
#
# Copies into each NEW_DIR the files of PREVIOUS_DIR that the new build
# lacks, so that a page of an earlier build still gets its hashed assets
# from a server of this build. A file stays while it belongs to one of the
# last KEEP_BUILDS builds before this one or to a build of the last
# KEEP_DAYS days, whichever keeps more. The modification time of a file is
# the time of the last build that made it, and the ledger lists the build
# times. The build fails when the previous image has none of the files,
# unless CARRY_FIRST_BUILD=1. CARRY_NOW sets the build time, for tests.
set -eu

KEEP_BUILDS=3
KEEP_DAYS=7

fail() {
	echo "carry-assets: $*" >&2
	exit 1
}

[ $# -ge 4 ] && [ $(($# % 2)) -eq 0 ] || fail "usage: carry-assets.sh PREVIOUS_LEDGER NEW_LEDGER PREVIOUS_DIR NEW_DIR [PREVIOUS_DIR NEW_DIR]..."
prev_ledger=$1
new_ledger=$2
shift 2

now=${CARRY_NOW:-$(date +%s)}

found=
[ -f "$prev_ledger" ] && found=1
set -- "$@"
n=$#
i=0
while [ $i -lt $n ]; do
	prev_dir=$1
	new_dir=$2
	shift 2
	set -- "$@" "$prev_dir" "$new_dir"
	i=$((i + 2))
	[ -d "$new_dir" ] || fail "the new build has no $new_dir"
	if [ -d "$prev_dir" ]; then
		found=1
	elif [ -f "$prev_ledger" ]; then
		fail "the previous image has a ledger but no $prev_dir"
	fi
done
if [ -z "$found" ]; then
	[ "${CARRY_FIRST_BUILD:-}" = 1 ] || fail "the previous image has no assets; set CARRY_FIRST_BUILD=1 for the first build"
	echo "carry-assets: first build, nothing to carry" >&2
fi

cutoff=$((now - KEEP_DAYS * 86400))
if [ -f "$prev_ledger" ]; then
	nth=$(sort -rn -u "$prev_ledger" | sed -n "${KEEP_BUILDS}p")
	if [ -z "$nth" ]; then
		cutoff=0
	elif [ "$nth" -lt "$cutoff" ]; then
		cutoff=$nth
	fi
else
	cutoff=0
fi

kept=0
dropped=0
while [ $# -gt 0 ]; do
	prev_dir=$1
	new_dir=$2
	shift 2
	find "$new_dir" -type f -exec touch -d "@$now" {} +
	[ -d "$prev_dir" ] || continue
	list=$(mktemp)
	(cd "$prev_dir" && find . -type f) >"$list"
	while IFS= read -r rel; do
		[ -e "$new_dir/$rel" ] && continue
		if [ "$(stat -c %Y "$prev_dir/$rel")" -ge "$cutoff" ]; then
			mkdir -p "$(dirname "$new_dir/$rel")"
			cp -p "$prev_dir/$rel" "$new_dir/$rel"
			kept=$((kept + 1))
		else
			dropped=$((dropped + 1))
		fi
	done <"$list"
	rm -f "$list"
done

mkdir -p "$(dirname "$new_ledger")"
{
	echo "$now"
	if [ -f "$prev_ledger" ]; then
		awk -v c="$cutoff" '$1 >= c' "$prev_ledger"
	fi
} | sort -rn -u >"$new_ledger.tmp"
mv "$new_ledger.tmp" "$new_ledger"
echo "carry-assets: carried $kept files of earlier builds, dropped $dropped" >&2
