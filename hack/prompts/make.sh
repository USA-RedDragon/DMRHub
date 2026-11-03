#!/usr/bin/env bash
# Usage: make.sh encode AMBE_SERVER WORK_DIR
#        make.sh speak AMBE_SERVER WORK_DIR PYTHON MODEL_DIR
# Makes the voice sets of internal/prompts on the AMBE server AMBE_SERVER
# (host:port). encode takes the 8 kHz clips of the shipped sets from
# inputs.tar.zst, so it gives the same frames again. speak first speaks
# clips.tsv with tts.py and cuts and resamples it with prepare.sh. The
# decoded copies for a listening check go to WORK_DIR/out/VOICE/wav.
set -euo pipefail
mode=$1 server=$2 work=$3
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
mkdir -p "$work/8k"
case $mode in
encode)
	zstd -dc "$here/inputs.tar.zst" | tar -C "$work/8k" -xf -
	;;
speak)
	"$4" -I "$here/tts.py" "$here/clips.tsv" "$5" "$work/src"
	for dir in "$work"/src/*/; do
		"$here/prepare.sh" "$dir" "$work/8k/$(basename "$dir")"
	done
	;;
*)
	echo "usage: make.sh encode|speak AMBE_SERVER WORK_DIR [PYTHON MODEL_DIR]" >&2
	exit 2
	;;
esac
for dir in "$work"/8k/*/; do
	voice=$(basename "$dir")
	(cd "$root" && go run ./hack/prompts -ambe "$server" -in "$dir" -out "$work/out/$voice")
	mkdir -p "$root/internal/prompts/voices/$voice"
	cp "$work/out/$voice"/frames/* "$root/internal/prompts/voices/$voice/"
done
