#!/bin/bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage: hack/external/setup.sh [-d DIR] [NAME...]

Fetches and builds the external DMR programs that the docs in doc/external
and the external client tests use, at the commits in the pins table of this script,
into DIR. DIR defaults to $DMRHUB_EXT_DIR, or else
${XDG_CACHE_HOME:-~/.cache}/dmrhub-ext. With NAME arguments, it does only
those programs. A program that is already at its pin is not built again.

NAME is one of: venv mmdvmhost dmrgateway hblink3 hblink4 freedmr
mmdvm_bridge mmdvm_cm droidstar ipsc2mmdvm

The layout of DIR is the one that hack/stress-ext.sh reads:
  venv/bin/python             Python with the packages of HBlink3, HBlink4
                              and FreeDMR
  mmdvmhost/MMDVM-Host        MMDVMHost, with DMRIds.dat, NXDN.csv, RSSI.dat
  dmrgateway/DMRGateway
  hblink3/bridge.py
  hblink4/run_hblink.py
  freedmr/bridge_master.py
  mmdvm_bridge/opt/MMDVM_Bridge/MMDVM_Bridge.amd64 (a binary that DVSwitch
                              publishes; there is no source)
  mmdvm_cm/YSF2DMR/YSF2DMR
  droidstar/build/DroidStar   with the DROIDSTAR_E2E_AUTOCONNECT hook of
                              hack/droidstar-test.sh, and no vocoder
  ipsc2mmdvm/ipsc2mmdvm
  PINS.md                     the programs and commits in DIR

Use DIR with the external client tests:
  DMRHUB_STRESS_EXT_DIR=DIR DMRHUB_STRESS_AREAS=transports hack/stress.sh
  hack/freedmr-test.sh DIR/freedmr DIR/venv/bin/python
  hack/droidstar-test.sh DIR/droidstar/build/DroidStar

Environment: JOBS (make jobs, default 4).
Needs git, make, a C++ compiler, cmake, Go, python3 with venv, and Qt 6
(Base, Declarative, Multimedia) for DroidStar.
EOF
}

ext=${DMRHUB_EXT_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-ext}
case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
-d | --dir)
	if [ $# -lt 2 ]; then
		usage >&2
		exit 2
	fi
	ext=$2
	shift 2
	;;
esac
all=(venv mmdvmhost dmrgateway hblink3 hblink4 freedmr mmdvm_bridge mmdvm_cm droidstar ipsc2mmdvm)
for n in "$@"; do
	if [[ " ${all[*]} " != *" $n "* ]]; then
		echo "unknown program $n" >&2
		usage >&2
		exit 2
	fi
done
jobs=${JOBS:-4}
mkdir -p "$ext"
ext=$(realpath "$ext")

pins=(
	"mmdvmhost https://github.com/g4klx/MMDVMHost 9384d683c9993f67af52b062b1bf9d4df11b6446"
	"dmrgateway https://github.com/g4klx/DMRGateway d751e20ed70816b3a834d3521aea58f03dd0b398"
	"hblink3 https://github.com/HBLink-org/hblink3 1db27305c29024a47f6c2243cb44c5df47198c13"
	"hblink4 https://github.com/n0mjs710/HBlink4 2348c3bcc5cbee4e945652ffbdb0bc722a403425"
	"freedmr https://gitlab.hacknix.net/hacknix/FreeDMR.git 546165eb9a53e8c1616a71f74a5858a122c805af"
	"mmdvm_bridge https://github.com/DVSwitch/MMDVM_Bridge 285e51e98dca637eab296a8fe00879e39381f678"
	"mmdvm_cm https://github.com/juribeparada/MMDVM_CM 7328c9e9532d29cf4d9d7d24541d57f825207be4"
	"droidstar https://github.com/USA-RedDragon/DroidStar 0580c7d72b27d3a10c1cac34d712458ebb861f71"
	"qtserialport https://github.com/qt/qtserialport beb4c887f3b886447cf86592fa65b8b9c1d6e312"
	"ipsc2mmdvm https://github.com/USA-RedDragon/ipsc2mmdvm 2bf48267375200cf8a139c579a8fcb2575f419bf"
)

python_packages=(
	bitarray==3.12.3 bitstring==5.0.0 Twisted==26.4.0 dmr_utils3==0.1.31
	configparser==7.2.0 resettabletimer==1.0.0 setproctitle==1.3.8 Pyro5==5.17
	PyYAML==6.0.3 typing_extensions==4.16.0
)

pin() {
	local p
	for p in "${pins[@]}"; do
		read -r n u c <<<"$p"
		if [ "$n" = "$1" ]; then
			echo "$u $c"
			return
		fi
	done
	return 1
}

fetch() {
	local name=$1 url commit dir=$ext/$1
	read -r url commit <<<"$(pin "$name")"
	if [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null)" = "$commit" ]; then
		return 0
	fi
	rm -rf "$dir"
	git init -q "$dir"
	git -C "$dir" remote add origin "$url"
	git -C "$dir" fetch -q --depth 1 origin "$commit"
	git -C "$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD
	rm -f "$dir/.built"
}

built() { [ -f "$ext/$1/.built" ]; }
mark() { touch "$ext/$1/.built"; }

step_venv() {
	if [ ! -x "$ext/venv/bin/python" ]; then
		python3 -m venv "$ext/venv"
	fi
	"$ext/venv/bin/pip" install -q --disable-pip-version-check "${python_packages[@]}"
}

step_mmdvmhost() {
	fetch mmdvmhost
	built mmdvmhost && return 0
	make -C "$ext/mmdvmhost" -j"$jobs" MMDVM-Host
	mark mmdvmhost
}

step_dmrgateway() {
	fetch dmrgateway
	built dmrgateway && return 0
	make -C "$ext/dmrgateway" -j"$jobs"
	mark dmrgateway
}

step_hblink3() { fetch hblink3; }
step_hblink4() { fetch hblink4; }
step_freedmr() { fetch freedmr; }
step_mmdvm_bridge() { fetch mmdvm_bridge; }

step_mmdvm_cm() {
	fetch mmdvm_cm
	built mmdvm_cm && return 0
	make -C "$ext/mmdvm_cm/YSF2DMR" -j"$jobs"
	mark mmdvm_cm
}

step_droidstar() {
	fetch droidstar
	built droidstar && return 0
	local d=$ext/droidstar prefix=$ext/qtprefix qtargs=()
	if ! pkg-config --exists Qt6SerialPort; then
		fetch qtserialport
		if ! built qtserialport; then
			cmake -S "$ext/qtserialport" -B "$ext/qtserialport/build" -DCMAKE_INSTALL_PREFIX="$prefix" \
				-DCMAKE_BUILD_TYPE=Release -DQT_BUILD_EXAMPLES=OFF -DQT_BUILD_TESTS=OFF
			cmake --build "$ext/qtserialport/build" -j"$jobs"
			cmake --install "$ext/qtserialport/build"
			mark qtserialport
		fi
		qtargs=(-DCMAKE_PREFIX_PATH="$prefix" -DQt6SerialPort_DIR="$prefix/lib/cmake/Qt6SerialPort")
	fi
	local line
	line=$(grep -n '^ *USE_.*_VOCODER *$' "$d/CMakeLists.txt" | head -1 | cut -d: -f1)
	if [ -n "$line" ]; then
		sed -i "$((line - 2))s/^if(TRUE)/if(FALSE)/" "$d/CMakeLists.txt"
	fi
	if ! grep -q DROIDSTAR_E2E_AUTOCONNECT "$d/droidstar.cpp"; then
		sed -i '0,/^\tprocess_settings();$/s//\tprocess_settings();\n\tif (qEnvironmentVariableIsSet("DROIDSTAR_E2E_AUTOCONNECT")) {\n\t\tm_vocoder = "None";\n\t\tm_modem = "None";\n\t\tQTimer::singleShot(qEnvironmentVariableIntValue("DROIDSTAR_E2E_AUTOCONNECT"), this, SLOT(process_connect()));\n\t}/' "$d/droidstar.cpp"
		grep -q DROIDSTAR_E2E_AUTOCONNECT "$d/droidstar.cpp"
	fi
	cmake -S "$d" -B "$d/build" -DCMAKE_BUILD_TYPE=Release "${qtargs[@]}"
	cmake --build "$d/build" -j"$jobs"
	mark droidstar
}

step_ipsc2mmdvm() {
	fetch ipsc2mmdvm
	built ipsc2mmdvm && return 0
	(cd "$ext/ipsc2mmdvm" && CGO_ENABLED=0 go build -o ipsc2mmdvm .)
	mark ipsc2mmdvm
}

todo=("$@")
[ ${#todo[@]} -eq 0 ] && todo=("${all[@]}")
for n in "${todo[@]}"; do
	echo "== $n"
	"step_$n"
done

{
	echo "# External programs"
	echo
	echo "Made by hack/external/setup.sh of DMRHub."
	echo
	echo "| Program | Source | Commit |"
	echo "| --- | --- | --- |"
	for p in "${pins[@]}"; do
		read -r n u c <<<"$p"
		if [ -d "$ext/$n" ]; then
			echo "| $n | $u | $(git -C "$ext/$n" rev-parse --short HEAD) |"
		fi
	done
	if [ -x "$ext/venv/bin/python" ]; then
		echo
		echo "Python: $("$ext/venv/bin/python" --version 2>&1), ${python_packages[*]}"
	fi
} >"$ext/PINS.md"
echo "done: $ext"
