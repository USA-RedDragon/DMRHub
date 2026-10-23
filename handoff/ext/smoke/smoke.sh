#!/bin/bash
# Smoke test one external program against a throwaway DMRHub on 127.0.0.1.
# Usage: smoke.sh NAME   (runs itself again inside unshare -r -n)
# NAME: dmrgateway ysf2dmr mmdvm_bridge hblink3 hblink4 xlxd
set -uo pipefail
EXT=/home/user/work/ext
S=$EXT/smoke
PY=$EXT/venv/bin/python
CAP=$S/capture-dmrgateway-master.txt
CALL="$PY -I $EXT/scripts/hbrp_call.py"

if [ "${SMOKE_IN_NS:-}" != 1 ]; then
	exec unshare -r -n env SMOKE_IN_NS=1 bash "$0" "$@"
fi
ip link set lo up
name=$1
W=$EXT/run/smoke-$name
rm -rf "$W"
mkdir -p "$W"
cd "$W" || exit 1
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null; done; sleep 1; for p in "${pids[@]}"; do kill -9 "$p" 2>/dev/null; done; }
trap cleanup EXIT

start_hub() {
	cp $S/template.db DMRHub.db
	sqlite3 DMRHub.db < $S/seed.sql
	cp $S/hub-config.yaml config.yaml
	$EXT/dmrhub-bin/DMRHub -c config.yaml > hub.log 2>&1 &
	pids+=($!)
	sleep 4
}

connected() {
	sqlite3 DMRHub.db "select id, connected from repeaters where connected is not null and connected > '2000'" 2>/dev/null
}

hub_calls() {
	sed 's/\x1b\[[0-9;]*m//g' hub.log | grep -E "Transmission ended|Repeater connected|logged in|RPTO|OpenBridge|peer" | grep -v -e DBG -e GIN | tail -20
}

case $name in
dmrgateway)
	start_hub
	cat > MMDVM-Host.ini <<EOF
[General]
Callsign=KI5VMF
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
Location=Austin
Description=ext smoke
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
LocalPort=62042
GatewayAddress=127.0.0.1
GatewayPort=62041
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
Id=319186801
Timeout=10
RptAddress=127.0.0.1
RptPort=62042
LocalAddress=127.0.0.1
LocalPort=62041
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
Callsign=KI5VMF
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
Location=Austin
Description=ext smoke
URL=https://example.org
[XLX Network]
Enabled=0
[DMR Network 1]
Enabled=1
Name=DMRHub
Address=127.0.0.1
Port=62031
PassAllTG=1
PassAllTG=2
PassAllPC=1
PassAllPC=2
Password=passw0rd
Options=TS1=91;TS2=9;
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
	$EXT/dmrgateway/DMRGateway DMRGateway.ini > dmrgateway.log 2>&1 &
	pids+=($!)
	$EXT/mmdvmhost/MMDVM-Host MMDVM-Host.ini > mmdvmhost.log 2>&1 &
	pids+=($!)
	sleep 15
	echo "== net call from a second repeater to TG91, DMRGateway should pass it to MMDVMHost"
	$CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --options "TS1=91;TS2=9;"
	sleep 4
	echo "== DMRGateway log"; tail -25 dmrgateway.log
	echo "== MMDVMHost log"; grep -v "^$" mmdvmhost.log | tail -25
	;;
ysf2dmr)
	start_hub
	sed -e "s|^Id=1234567|Id=319186802|" -e "s|^Address=44.131.4.1|Address=127.0.0.1|" -e "s|^Password=PASSWORD|Password=passw0rd|" \
		-e "s|^StartupDstId=9990|StartupDstId=91|" -e "s|^StartupPC=1|StartupPC=0|" -e "s|^FileLevel=1|FileLevel=0|" \
		-e "s|^File=DMRIds.dat|File=$EXT/mmdvm_cm/YSF2DMR/DMRIds.dat|" -e "s|^TGListFile=TGList-DMR.txt|TGListFile=$EXT/mmdvm_cm/YSF2DMR/TGList-DMR.txt|" \
		-e "s|^Callsign=G9BF|Callsign=KI5VMF|" $EXT/mmdvm_cm/YSF2DMR/YSF2DMR.ini > YSF2DMR.ini
	$EXT/mmdvm_cm/YSF2DMR/YSF2DMR YSF2DMR.ini > ysf2dmr.log 2>&1 &
	pids+=($!)
	sleep 15
	echo "== YSF2DMR log"; tail -25 ysf2dmr.log
	;;
mmdvm_bridge)
	start_hub
	B=$EXT/mmdvm_bridge/opt/MMDVM_Bridge
	sed -e "s|^Id=1234567|Id=319186803|" -e "s|^Callsign=N0CALL|Callsign=KI5VMF|" -e "s|^FileLevel=2|FileLevel=0|" -e "s|^FilePath=/var/log/mmdvm|FilePath=$W|" \
		-e "s|^File=/var/lib/mmdvm/DMRIds.dat|File=$EXT/mmdvmhost/DMRIds.dat|" -e "s|^File=/var/lib/mmdvm/NXDN.csv|File=$EXT/mmdvmhost/NXDN.csv|" $B/MMDVM_Bridge.ini |
		awk '/^\[DMR\]$/{s="dmr"} /^\[DMR Network\]$/{s="net"} /^\[/{if($0!="[DMR]"&&$0!="[DMR Network]")s=""}
			s=="dmr"&&/^Enable=/{print "Enable=1";next}
			s=="net"&&/^Enable=/{print "Enable=1";next}
			s=="net"&&/^Address=/{print "Address=127.0.0.1";next}
			s=="net"&&/^Local=/{print "Local=62043";next}
			s=="net"&&/^Slot1=/{print "Slot1=1";next}
			s=="net"&&/^# Options=/{print "Options=TS1=91;TS2=9;";next}
			{print}' > MMDVM_Bridge.ini
	sed -e "s|/var/log/dvswitch/|$W/|" $B/DVSwitch.ini > DVSwitch.ini
	$B/MMDVM_Bridge.amd64 MMDVM_Bridge.ini > mmdvm_bridge.log 2>&1 &
	pids+=($!)
	sleep 15
	$CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --dst 9 --slot2
	sleep 4
	echo "== MMDVM_Bridge log"; tail -25 mmdvm_bridge.log
	;;
hblink3)
	start_hub
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
PORT: 62051
NETWORK_ID: 3001
PASSPHRASE: passw0rd
TARGET_IP: 127.0.0.1
TARGET_PORT: 62035
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
PORT: 62053
MASTER_IP: 127.0.0.1
MASTER_PORT: 62031
PASSPHRASE: passw0rd
CALLSIGN: KI5VMF
RADIO_ID: 319186804
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
OPTIONS: TS1=91;TS2=9;
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
PORT: 62054
PASSPHRASE: passw0rd
GROUP_HANGTIME: 5
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
EOF
	cat > rules.py <<'EOF'
BRIDGES = {
    'OBP91': [
        {'SYSTEM': 'OBP-DMRHUB', 'TS': 1, 'TGID': 91, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'MASTER-LOCAL', 'TS': 2, 'TGID': 1001, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
    'PEER91': [
        {'SYSTEM': 'PEER-DMRHUB', 'TS': 1, 'TGID': 91, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'MASTER-LOCAL', 'TS': 2, 'TGID': 1002, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
}
UNIT = []
EOF
	echo '{"rptrs": []}' > peer_ids.json; echo '{"users": []}' > subscriber_ids.json; echo '{"talkgroups": []}' > talkgroup_ids.json
	$PY -E -s $EXT/hblink3/bridge.py -c "$W/hblink.cfg" -r "$W/rules.py" > hblink3.log 2>&1 &
	pids+=($!)
	sleep 12
	$CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --options "TS1=91;TS2=9;"
	sleep 4
	echo "== HBlink3 log"; grep -v "^$" hblink3.log | grep -v -i "ping\|pong\|maintenance" | tail -30
	;;
hblink4)
	H4LAT=${HBLINK4_LAT:-'"30.26720"'}; H4LON=${HBLINK4_LON:-'"-97.74310"'}
	start_hub
	cat > config.json <<EOF
{
 "global": {"bind_ipv4": "127.0.0.1", "port_ipv4": 62061, "disable_ipv6": true, "max_missed": 3, "timeout_duration": 30,
  "logging": {"file": "$W/hblink4.file.log", "console_level": "DEBUG", "file_level": "DEBUG", "retention_days": 1},
  "stats_interval": 60, "report_stats": false, "stream_timeout": 2.0, "stream_hang_time": 10.0, "user_cache": {"timeout": 600}},
 "dashboard": {"enabled": false},
 "repeater_configurations": {"default": {"passphrase": "passw0rd", "slot1_talkgroups": [91], "slot2_talkgroups": [9]}},
 "outbound_connections": [{"enabled": true, "name": "DMRHub", "address": "127.0.0.1", "port": 62031, "passphrase": "passw0rd",
  "radio_id": 319186805, "callsign": "KI5VMF", "rx_frequency": 438800000, "tx_frequency": 438800000, "power": 1, "colorcode": 1,
  "latitude": $H4LAT, "longitude": $H4LON, "height": 0, "location": "Austin", "description": "ext smoke", "url": "https://example.org",
  "software_id": "HBlink4", "package_id": "HBlink4", "options": "TS1=91", "unit_calls_enabled": true}],
 "openbridge_connections": [{"enabled": true, "name": "DMRHub-OBP", "network_id": 3002, "local_address": "127.0.0.1", "local_port": 62052,
  "target_address": "127.0.0.1", "target_port": 62035, "passphrase": "passw0rd", "preserve_source_peer": true,
  "talkgroup_slots": {"9": "2"}, "description": "smoke"}]
}
EOF
	(cd $EXT/hblink4 && exec $PY -E -s run_hblink.py "$W/config.json") > hblink4.log 2>&1 &
	pids+=($!)
	sleep 12
	$CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --options "TS1=91;TS2=9;" && sleep 3 && $CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --dst 9 --slot2
	sleep 4
	echo "== HBlink4 log"; grep -v "^$" hblink4.log | grep -v -i "ping\|pong\|keepalive" | tail -30
	;;
adn)
	start_hub
	mariadb-install-db --datadir=$W/db --user=root --auth-root-authentication-method=normal > mariadb-install.log 2>&1
	mariadbd --datadir=$W/db --socket=$W/db.sock --port=3306 --bind-address=127.0.0.1 --user=root --pid-file=$W/db.pid > mariadb.log 2>&1 &
	pids+=($!)
	sleep 6
	mariadb -S $W/db.sock -u root -e "CREATE DATABASE hbmon; DELETE FROM mysql.global_priv WHERE User=''; FLUSH PRIVILEGES; CREATE USER 'adn'@'%' IDENTIFIED BY 'adnpass'; CREATE USER 'adn'@'localhost' IDENTIFIED BY 'adnpass'; GRANT ALL ON hbmon.* TO 'adn'@'%'; GRANT ALL ON hbmon.* TO 'adn'@'localhost';"
	$PY -I $EXT/scripts/adn_config.py $EXT/adn/adn-server.example.yaml $W/adn-server.yaml $W
	(cd $EXT/adn && exec $PY -E -s adn-server.py -c "$W/adn-server.yaml") > adn.log 2>&1 &
	pids+=($!)
	sleep 15
	echo "== ADN-side client 319186807 sends TG91 TS1, hub-side client 319186808 listens"
	$CALL 127.0.0.1 62031 319186808 passw0rd --options "TS1=91;TS2=9;" --listen 12 > hub-listener.log 2>&1 &
	sleep 2
	$CALL 127.0.0.1 62071 319186807 passw0rd --capture $CAP
	sleep 11
	cat hub-listener.log
	echo "== hub-side client 319186809 sends TG91 TS1, ADN-side client 319186806 listens"
	$CALL 127.0.0.1 62071 319186806 passw0rd --listen 12 > adn-listener.log 2>&1 &
	sleep 2
	$CALL 127.0.0.1 62031 319186809 passw0rd --capture $CAP --options "TS1=91;TS2=9;"
	sleep 11
	cat adn-listener.log
	echo "== ADN log"; grep -v "^$" adn.log | grep -v -i "ping\|pong\|keepalive\|BCKA" | tail -40
	;;
xlxd)
	mkdir -p $EXT/xlxd-run
	printf "3191868;KI5VMF;Jacob\n319186809;KI5VMF;Jacob\n" > $EXT/xlxd-run/dmrid.dat
	$EXT/xlxd/src/xlxd XLX000 127.0.0.1 127.0.0.1 > xlxd.log 2>&1 &
	pids+=($!)
	sleep 5
	echo "== hbrp_call.py client to xlxd DMRMMDVM port 62030 (xlxd is a master; DMRHub cannot peer with it)"
	$CALL 127.0.0.1 62030 3191868 passw0rd --capture $CAP --dst 4001 --listen 3
	sleep 2
	echo "== xlxd log"; tail -25 xlxd.log
	exit 0
	;;
*)
	echo "unknown $name"; exit 2;;
esac
echo "== DMRHub repeaters connected"; connected
echo "== DMRHub log"; hub_calls
