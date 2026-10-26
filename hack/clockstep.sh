#!/bin/bash
# Runs the tests of the clockstep build tag, which step the wall clock, each
# package in its own throwaway VM, so that the clock of this machine never
# moves. The test binary is the init of the VM.
#
# Needs qemu-system-x86_64 with KVM, cpio and zstd.
#
# Environment:
#   KERNEL    kernel of the VM (default: /usr/lib/modules/$(uname -r)/vmlinuz)
#   PACKAGES  packages to test (default: every package with a clockstep test)
#   WORKDIR   where the VM images go (default: artifacts/clockstep)
#   TAGS      more build tags, comma separated (default: none)
#   RUN       tests to run (default: ^TestClockStep)
#   ARGS      more arguments of the test binary (default: none)
#   VMENV     environment of the tests, space separated NAME=value words
#   OUT       directory for what the tests write to DMRHUB_CLOCK_STEP_OUT in
#             the VM, and for the whole console output, console.log; the
#             tests must call clockstep.Main from TestMain (default: none)
#   MEM       memory of the VM (default: 2G)
#   SMP       CPUs of the VM (default: 2)
set -euo pipefail

cd "$(dirname "$0")/.."
kernel=${KERNEL:-/usr/lib/modules/$(uname -r)/vmlinuz}
work=${WORKDIR:-artifacts/clockstep}
tags=clockstep${TAGS:+,$TAGS}
run=${RUN:-^TestClockStep}
if [ -z "${PACKAGES:-}" ]; then
    PACKAGES=$(grep -rl --include='*_test.go' '^//go:build clockstep' internal | xargs -n1 dirname | sort -u | sed 's|^|./|')
fi

status=0
for pkg in $PACKAGES; do
    root="$work/root"
    rm -rf "$root"
    mkdir -p "$root/tmp"
    CGO_ENABLED=0 go test -tags "$tags" -c -o "$root/init" "$pkg"
    (cd "$root" && printf '%s\n' . tmp init | cpio --quiet -o -H newc) | zstd -q -f -o "$work/initrd.zst"
    disk=()
    vmenv=${VMENV:-}
    if [ -n "${OUT:-}" ]; then
        mkdir -p "$OUT"
        truncate -s 0 "$work/out.img"
        truncate -s 1G "$work/out.img"
        disk=(-drive "file=$work/out.img,format=raw,if=virtio")
        vmenv="$vmenv DMRHUB_CLOCK_STEP_OUT=/out"
    fi
    echo "== $pkg"
    # shellcheck disable=SC2086
    out=$(qemu-system-x86_64 -enable-kvm -cpu host -smp "${SMP:-2}" -m "${MEM:-2G}" -nographic -no-reboot \
        -kernel "$kernel" -initrd "$work/initrd.zst" "${disk[@]}" \
        -append "console=ttyS0 quiet panic=-1 DMRHUB_CLOCK_STEP=1 $vmenv -- -test.v -test.run $run ${ARGS:-}" | tr -d '\r')
    printf '%s\n' "$out" | grep -E '^(=== RUN|--- |    |PASS$|FAIL$|ok )' || true
    if [ -n "${OUT:-}" ]; then
        printf '%s\n' "$out" > "$OUT/console.log"
        tar -x -f "$work/out.img" -C "$OUT" || status=1
    fi
    if ! printf '%s\n' "$out" | grep -qx PASS; then
        status=1
    fi
done
rm -rf "$work"
exit $status
