#!/usr/bin/env bash
# Usage: prepare.sh SRC_DIR OUT_DIR
# Converts each SRC_DIR/NAME.wav to mono 16-bit PCM at 8 kHz in OUT_DIR,
# with the silence before the speech cut and the clip ended at the first
# 0.3 s of silence, for the prompts command.
set -euo pipefail
src=$1 out=$2
mkdir -p "$out"
for f in "$src"/*.wav; do
	sox -V1 "$f" -c 1 -b 16 "$out/$(basename "$f")" \
		silence 1 0.005 -50d 1 0.3 -50d \
		highpass 60 rate -v 8000 dither -s
done
