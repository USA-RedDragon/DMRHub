#!/bin/bash
# Starts one external DMR program for the transport area of the stress
# battery, with a configuration that points it at the hub of the test, and
# runs it in the foreground until it is killed. The test runs inside
# unshare -r -n, so the program reaches only loopback.
# Usage: stress-ext.sh NAME WORKDIR HUB_MMDVM_PORT HUB_OB_PORT ID TG OB_ID OB_TG
# NAME: dmrgateway hblink3 hblink4 mmdvm_bridge ysf2dmr
# DMRHUB_EXT_CALLSIGN, DMRHUB_EXT_PASSWORD, DMRHUB_EXT_LOCATION and
# DMRHUB_EXT_DESCRIPTION change what MMDVMHost and DMRGateway send.
# hack/external/setup.sh builds the programs into the directory that
# DMRHUB_STRESS_EXT_DIR names, by default the one that setup.sh uses.
set -u
EXT=${DMRHUB_STRESS_EXT_DIR:-${DMRHUB_EXT_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/dmrhub-ext}}
PY=$EXT/venv/bin/python
name=$1 W=$2 HUB=$3 OB=$4 ID=$5 TG=$6 OBID=$7 OBTG=$8
mkdir -p "$W"
cd "$W" || exit 1
H4LAT='"30.26720"'; H4LON='"-97.74310"'
CALL=${DMRHUB_EXT_CALLSIGN:-KI5VMF} PASS=${DMRHUB_EXT_PASSWORD:-passw0rd}
LOC=${DMRHUB_EXT_LOCATION:-Austin} DESC=${DMRHUB_EXT_DESCRIPTION:-ext smoke}
case $name in
dmrgateway)
	cat > MMDVM-Host.ini <<EOF
[General]
Callsign=$CALL
Id=3191868
Timeout=180
Duplex=1
RFModeHang=10
NetModeHang=3
Daemon=0
[Info]
RXFrequency=438800000
TXFrequency=438800000
Power=1
Latitude=30.2672
Longitude=-97.7431
Height=0
Location=$LOC
Description=$DESC
URL=https://example.org
[Log]
MQTTLevel=0
DisplayLevel=1
[MQTT]
Host=127.0.0.1
Port=1883
Auth=0
Username=mmdvm
Password=mmdvm
Keepalive=60
Name=host
[CW Id]
Enable=0
Time=10
[DMR Id Lookup]
File=$EXT/mmdvmhost/DMRIds.dat
Time=24
[NXDN Id Lookup]
File=$EXT/mmdvmhost/NXDN.csv
Time=24
[Modem]
Protocol=null
RXFrequency=438800000
TXFrequency=438800000
TXInvert=1
RXInvert=0
PTTInvert=0
TXDelay=100
RXOffset=0
TXOffset=0
DMRDelay=0
RXLevel=50
TXLevel=50
RXDCOffset=0
TXDCOffset=0
RFLevel=100
RSSIMappingFile=$EXT/mmdvmhost/RSSI.dat
UseCOSAsLockout=0
Trace=0
Debug=0
[Transparent Data]
Enable=0
[D-Star]
Enable=0
[DMR]
Enable=1
Beacons=0
ColorCode=1
SelfOnly=0
EmbeddedLCOnly=0
DumpTAData=1
CallHang=3
TXHang=4
[DMR Trunking]
Enable=0
[System Fusion]
Enable=0
[P25]
Enable=0
[NXDN]
Enable=0
[POCSAG]
Enable=0
[FM]
Enable=0
[D-Star Network]
Enable=0
[DMR Network]
Enable=1
LocalAddress=127.0.0.1
LocalPort=$((HUB+12))
GatewayAddress=127.0.0.1
GatewayPort=$((HUB+11))
Jitter=360
Slot1=1
Slot2=1
Debug=0
[System Fusion Network]
Enable=0
[P25 Network]
Enable=0
[NXDN Network]
Enable=0
[POCSAG Network]
Enable=0
[FM Network]
Enable=0
[Lock File]
Enable=0
[Remote Control]
Enable=0
EOF
	cat > DMRGateway.ini <<EOF
[General]
Id=$ID
Timeout=10
RptAddress=127.0.0.1
RptPort=$((HUB+12))
LocalAddress=127.0.0.1
LocalPort=$((HUB+11))
RuleTrace=1
Daemon=0
TrunkingEnabled=0
Debug=0
[Log]
DisplayLevel=1
MQTTLevel=0
[Voice]
Enabled=0
[Info]
Callsign=$CALL
TXFrequency=438800000
RXFrequency=438800000
Power=1
ColorCode=1
Duplex=1
Slot1=1
Slot2=1
Latitude=30.2672
Longitude=-97.7431
Height=0
Location=$LOC
Description=$DESC
URL=https://example.org
[XLX Network]
Enabled=0
[DMR Network 1]
Enabled=1
Name=DMRHub
Address=127.0.0.1
Port=$HUB
PassAllTG=1
PassAllTG=2
PassAllPC=1
PassAllPC=2
Password=$PASS
Options=TS1=$TG;
Location=1
Debug=0
[DMR Network 2]
Enabled=0
[DMR Network 3]
Enabled=0
[DMR Network 4]
Enabled=0
[DMR Network 5]
Enabled=0
[GPSD]
Enable=0
[APRS]
Enable=0
[MQTT]
Host=127.0.0.1
Port=1883
Auth=0
Username=mmdvm
Password=mmdvm
Keepalive=60
Name=dmr-gateway
[Dynamic TG Control]
Enabled=0
[Remote Commands]
Enable=0
EOF
	$EXT/mmdvmhost/MMDVM-Host MMDVM-Host.ini > mmdvmhost.log 2>&1 &
	trap 'kill $!' EXIT
	exec $EXT/dmrgateway/DMRGateway DMRGateway.ini
	;;
hblink3)
	cat > hblink.cfg <<EOF
[GLOBAL]
PATH: $W/
PING_TIME: 5
MAX_MISSED: 3
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL

[REPORTS]
REPORT: False
REPORT_INTERVAL: 60
REPORT_PORT: 4321
REPORT_CLIENTS: 127.0.0.1

[LOGGER]
LOG_FILE: /dev/null
LOG_HANDLERS: console
LOG_LEVEL: DEBUG
LOG_NAME: HBlink

[ALIASES]
TRY_DOWNLOAD: False
PATH: $W/
PEER_FILE: peer_ids.json
SUBSCRIBER_FILE: subscriber_ids.json
TGID_FILE: talkgroup_ids.json
PEER_URL: http://127.0.0.1:1/none
SUBSCRIBER_URL: http://127.0.0.1:1/none
STALE_DAYS: 7

[OBP-DMRHUB]
MODE: OPENBRIDGE
ENABLED: True
IP: 127.0.0.1
PORT: $((HUB+21))
NETWORK_ID: $OBID
PASSPHRASE: passw0rd
TARGET_IP: 127.0.0.1
TARGET_PORT: $OB
BOTH_SLOTS: True
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL

[PEER-DMRHUB]
MODE: PEER
ENABLED: True
LOOSE: False
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: $((HUB+22))
MASTER_IP: 127.0.0.1
MASTER_PORT: $HUB
PASSPHRASE: passw0rd
CALLSIGN: KI5VMF
RADIO_ID: $ID
RX_FREQ: 438800000
TX_FREQ: 438800000
TX_POWER: 1
COLORCODE: 1
SLOTS: 3
LATITUDE: 30.2672
LONGITUDE: -97.7431
HEIGHT: 0
LOCATION: Austin
DESCRIPTION: ext smoke
URL: https://example.org
SOFTWARE_ID: 20170620
PACKAGE_ID: MMDVM_HBlink
GROUP_HANGTIME: 5
OPTIONS: TS1=$TG;
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL

[MASTER-LOCAL]
MODE: MASTER
ENABLED: True
REPEAT: True
MAX_PEERS: 1
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: $((HUB+23))
PASSPHRASE: passw0rd
GROUP_HANGTIME: 5
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
EOF
	cat > rules.py <<EOF
BRIDGES = {
    'OBP91': [
        {'SYSTEM': 'OBP-DMRHUB', 'TS': 1, 'TGID': $TG, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'MASTER-LOCAL', 'TS': 2, 'TGID': 1001, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
    'PEER91': [
        {'SYSTEM': 'PEER-DMRHUB', 'TS': 1, 'TGID': $TG, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'MASTER-LOCAL', 'TS': 2, 'TGID': 1002, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
}
UNIT = []
EOF
	echo '{"rptrs": []}' > peer_ids.json; echo '{"users": []}' > subscriber_ids.json; echo '{"talkgroups": []}' > talkgroup_ids.json
	exec $PY -E -s $EXT/hblink3/bridge.py -c "$W/hblink.cfg" -r "$W/rules.py"
	;;
hblink4)
	cat > config.json <<EOF
{
 "global": {"bind_ipv4": "127.0.0.1", "port_ipv4": $((HUB+31)), "disable_ipv6": true, "max_missed": 3, "timeout_duration": 30,
  "logging": {"file": "$W/hblink4.file.log", "console_level": "DEBUG", "file_level": "DEBUG", "retention_days": 1},
  "stats_interval": 60, "report_stats": false, "stream_timeout": 2.0, "stream_hang_time": 10.0, "user_cache": {"timeout": 600}},
 "dashboard": {"enabled": false},
 "repeater_configurations": {"default": {"passphrase": "passw0rd", "slot1_talkgroups": [$TG], "slot2_talkgroups": [$OBTG]}},
 "outbound_connections": [{"enabled": true, "name": "DMRHub", "address": "127.0.0.1", "port": $HUB, "passphrase": "passw0rd",
  "radio_id": $ID, "callsign": "KI5VMF", "rx_frequency": 438800000, "tx_frequency": 438800000, "power": 1, "colorcode": 1,
  "latitude": $H4LAT, "longitude": $H4LON, "height": 0, "location": "Austin", "description": "ext smoke", "url": "https://example.org",
  "software_id": "HBlink4", "package_id": "HBlink4", "options": "TS1=$TG", "unit_calls_enabled": true}],
 "openbridge_connections": [{"enabled": true, "name": "DMRHub-OBP", "network_id": $OBID, "local_address": "127.0.0.1", "local_port": $((HUB+32)),
  "target_address": "127.0.0.1", "target_port": $OB, "passphrase": "passw0rd", "preserve_source_peer": true,
  "talkgroup_slots": {"$OBTG": "1"}, "description": "smoke"}]
}
EOF
	cd $EXT/hblink4 && exec $PY -E -s run_hblink.py "$W/config.json"
	;;
mmdvm_bridge)
	B=$EXT/mmdvm_bridge/opt/MMDVM_Bridge
	sed -e "s|^Id=1234567|Id=$ID|" -e "s|^Callsign=N0CALL|Callsign=KI5VMF|" -e "s|^FileLevel=2|FileLevel=0|" -e "s|^FilePath=/var/log/mmdvm|FilePath=$W|" \
		-e "s|^File=/var/lib/mmdvm/DMRIds.dat|File=$EXT/mmdvmhost/DMRIds.dat|" -e "s|^File=/var/lib/mmdvm/NXDN.csv|File=$EXT/mmdvmhost/NXDN.csv|" $B/MMDVM_Bridge.ini |
		awk '/^\[DMR\]$/{s="dmr"} /^\[DMR Network\]$/{s="net"} /^\[/{if($0!="[DMR]"&&$0!="[DMR Network]")s=""}
			s=="dmr"&&/^Enable=/{print "Enable=1";next}
			s=="net"&&/^Enable=/{print "Enable=1";next}
			s=="net"&&/^Address=/{print "Address=127.0.0.1";next}
			s=="net"&&/^Port=/{print "Port='"$HUB"'";next}
			s=="net"&&/^Local=/{print "Local='"$((HUB+41))"'";next}
			s=="net"&&/^Slot1=/{print "Slot1=1";next}
			s=="net"&&/^# Options=/{print "Options=TS2='"$TG"';";next}
			{print}' > MMDVM_Bridge.ini
	sed -e "s|/var/log/dvswitch/|$W/|" $B/DVSwitch.ini > DVSwitch.ini
	exec $B/MMDVM_Bridge.amd64 MMDVM_Bridge.ini
	;;
ysf2dmr)
	sed -e "s|^Id=1234567|Id=$ID|" -e "s|^Address=44.131.4.1|Address=127.0.0.1|" -e "s|^Port=62031|Port=$HUB|" -e "s|^Password=PASSWORD|Password=passw0rd|" \
		-e "s|^StartupDstId=9990|StartupDstId=$TG|" -e "s|^StartupPC=1|StartupPC=0|" -e "s|^FileLevel=1|FileLevel=0|" \
		-e "s|^File=DMRIds.dat|File=$EXT/mmdvm_cm/YSF2DMR/DMRIds.dat|" -e "s|^TGListFile=TGList-DMR.txt|TGListFile=$EXT/mmdvm_cm/YSF2DMR/TGList-DMR.txt|" \
		-e "s|^Callsign=G9BF|Callsign=KI5VMF|" $EXT/mmdvm_cm/YSF2DMR/YSF2DMR.ini > YSF2DMR.ini
	exec $EXT/mmdvm_cm/YSF2DMR/YSF2DMR YSF2DMR.ini
	;;
*)
	echo "unknown $name" >&2
	exit 2
	;;
esac
