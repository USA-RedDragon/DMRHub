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
set -euo pipefail

cd "$(dirname "$0")/.."
kernel=${KERNEL:-/usr/lib/modules/$(uname -r)/vmlinuz}
work=${WORKDIR:-artifacts/clockstep}
if [ -z "${PACKAGES:-}" ]; then
    PACKAGES=$(grep -rl --include='*_test.go' '^//go:build clockstep' internal | xargs -n1 dirname | sort -u | sed 's|^|./|')
fi

status=0
for pkg in $PACKAGES; do
    root="$work/root"
    rm -rf "$root"
    mkdir -p "$root/tmp"
    CGO_ENABLED=0 go test -tags clockstep -c -o "$root/init" "$pkg"
    (cd "$root" && printf '%s\n' . tmp init | cpio --quiet -o -H newc) | zstd -q -f -o "$work/initrd.zst"
    echo "== $pkg"
    out=$(qemu-system-x86_64 -enable-kvm -cpu host -smp 2 -m 2G -nographic -no-reboot \
        -kernel "$kernel" -initrd "$work/initrd.zst" \
        -append "console=ttyS0 quiet panic=-1 DMRHUB_CLOCK_STEP=1 -- -test.v -test.run ^TestClockStep" | tr -d '\r')
    printf '%s\n' "$out" | grep -E '^(=== RUN|--- |    |PASS$|FAIL$|ok )' || true
    if ! printf '%s\n' "$out" | grep -qx PASS; then
        status=1
    fi
done
rm -rf "$work"
exit $status
