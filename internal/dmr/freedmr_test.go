// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>
package dmr_test

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The environment of TestExternalFreeDMR: the FreeDMR source directory,
// which holds bridge_master.py, and the Python that runs it.
const (
	freeDMREnv       = "DMRHUB_FREEDMR"
	freeDMRPythonEnv = "DMRHUB_FREEDMR_PYTHON"
)

// freeDMRServerID is the SERVER_ID of the FreeDMR server and the
// NETWORK_ID of its link to DMRHub, so the peer ID in DMRHub. FreeDMR sends
// its SERVER_ID in the repeater field of each DMRE (hblink.py send_system)
// and checks that the field of a packet it gets is its NETWORK_ID.
const freeDMRServerID = 3129

const freeDMRConfig = `[GLOBAL]
PATH: ./
PING_TIME: 5
MAX_MISSED: 3
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
GEN_STAT_BRIDGES: True
ALLOW_NULL_PASSPHRASE: False
ANNOUNCEMENT_LANGUAGES: en_GB
SERVER_ID: %[1]d
DATA_GATEWAY: False
VALIDATE_SERVER_IDS: False
ENABLE_API: False

[REPORTS]
REPORT: False
REPORT_INTERVAL: 60
REPORT_PORT: %[5]d
REPORT_CLIENTS: 127.0.0.1

[LOGGER]
LOG_FILE: /dev/null
LOG_HANDLERS: console
LOG_LEVEL: DEBUG
LOG_NAME: FreeDMR

[ALIASES]
TRY_DOWNLOAD: False
PATH: ./
PEER_FILE: peer_ids.json
SUBSCRIBER_FILE: subscriber_ids.json
TGID_FILE: talkgroup_ids.json
PEER_URL: http://127.0.0.1:1/none
SUBSCRIBER_URL: http://127.0.0.1:1/none
TGID_URL: http://127.0.0.1:1/none
SERVER_ID_URL: http://127.0.0.1:1/none
CHECKSUM_URL: http://127.0.0.1:1/none
STALE_DAYS: 7
SERVER_ID_FILE: server_ids.tsv
CHECKSUM_FILE: file_checksums.json
KEYS_FILE: keys.json
SUB_MAP_FILE: sub_map.pkl
LOCAL_SUBSCRIBER_FILE: local_subscribers.json

[ALLSTAR]
ENABLED: False

[OBP-DMRHUB]
MODE: OPENBRIDGE
ENABLED: True
IP: 127.0.0.1
PORT: %[2]d
NETWORK_ID: %[1]d
PASSPHRASE: %[6]s
TARGET_IP: 127.0.0.1
TARGET_PORT: %[3]d
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL
RELAX_CHECKS: True
ENHANCED_OBP: True
PROTO_VER: 5

[SYSTEM]
MODE: MASTER
ENABLED: True
REPEAT: True
MAX_PEERS: 1
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: %[4]d
PASSPHRASE: %[7]s
GROUP_HANGTIME: 5
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
DEFAULT_UA_TIMER: 10
SINGLE_MODE: False
VOICE_IDENT: False
DIAL_A_TG: True
DYNAMIC_TG_ROUTING: True
NETWORK_DIRECT_DIAL_SLOT: 1
TS1_STATIC: 91
TS2_STATIC:
DEFAULT_DIAL_TS1: 0
DEFAULT_DIAL_TS2: 0
DEFAULT_REFLECTOR: 0
ANNOUNCEMENT_LANGUAGE: en_GB
GENERATOR: 1
ALLOW_UNREG_ID: True
PROXY_CONTROL: False
OVERRIDE_IDENT_TG:
`

func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	c, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	addr, ok := c.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	return addr.Port
}

type lockedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.b.Write(p)
	if err != nil {
		return n, fmt.Errorf("writing the log: %w", err)
	}
	return n, nil
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestExternalFreeDMR links a real FreeDMR server (bridge_master.py) to
// the hub over enhanced OpenBridge version 5. A call on TG 91 from a
// repeater on the hub must reach a repeater on FreeDMR, and back, and so
// must an SMS to a radio on each, and the hub must drop forged packets on
// the link. FreeDMR carries no private voice over OpenBridge
// (bridge_master.py takes only unit data from it, and makes a private call
// from a repeater a Dial-A-TG). FreeDMR must keep sending to the hub after
// a minute, which it does only
// while the hub sends BCKA keepalives (bridge_master.py stops sending to an
// enhanced link after 60 s without one). It runs only when DMRHUB_FREEDMR
// names the FreeDMR source and the process is in a network namespace with
// only loopback, as hack/freedmr-test.sh runs it. Calls are sent in real
// time, one burst every 60 ms: bridge_master.py drops the packets of a
// stream from a repeater after 18 when they come faster than 25 a second.
// DMRHUB_FREEDMR_LOG=1 prints the log of FreeDMR.
func TestExternalFreeDMR(t *testing.T) {
	t.Parallel()
	src := os.Getenv(freeDMREnv)
	if src == "" {
		t.Skip("set " + freeDMREnv + " to the FreeDMR source and run hack/freedmr-test.sh")
	}
	require.NoError(t, testutils.RequireIsolatedNetwork(), "run hack/freedmr-test.sh, which runs the test in a network namespace")
	python := os.Getenv(freeDMRPythonEnv)
	if python == "" {
		python = "python3"
	}

	stack := testutils.SetupIntegrationStack(t, testutils.SQLiteMemoryBackend())
	stack.SeedUser(t, mxRadio, "N0CALL")
	stack.SeedTalkgroup(t, 91, "TG91")
	stack.SeedMMDVMRepeater(t, mxTGRpt, mxRadio, testPassword)
	stack.AssignTS1StaticTG(t, mxTGRpt, 91)

	obpPort, masterPort, reportPort := freePort(t), freePort(t), freePort(t)
	stack.SeedPeer(t, freeDMRServerID, mxRadio, obPassword, "127.0.0.1", obpPort, true, true)
	require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", freeDMRServerID).Update("enhanced", true).Error)
	stack.SeedPeerRule(t, freeDMRServerID, true, 1, 99999999)
	stack.SeedPeerRule(t, freeDMRServerID, false, 1, 99999999)
	stack.StartServers(t)

	_, hubPort, err := net.SplitHostPort(stack.OpenBridgeAddr)
	require.NoError(t, err)
	port, err := strconv.Atoi(hubPort)
	require.NoError(t, err)
	dir := t.TempDir()
	cfg := fmt.Sprintf(freeDMRConfig, freeDMRServerID, obpPort, port, masterPort, reportPort, obPassword, testPassword)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "freedmr.cfg"), []byte(cfg), 0o600))
	logs := &lockedLog{}
	cmd := exec.CommandContext(t.Context(), python, "-E", "-s", filepath.Join(src, "bridge_master.py"), "-c", filepath.Join(dir, "freedmr.cfg")) //nolint:gosec
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, logs, logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() || os.Getenv("DMRHUB_FREEDMR_LOG") != "" {
			t.Log(logs.String())
		}
	})

	var remote *testutils.MMDVMClient
	require.Eventually(t, func() bool {
		c := testutils.NewMMDVMClient(liveRemoteHotspot, "", testPassword)
		if c.Connect(fmt.Sprintf("127.0.0.1:%d", masterPort)) != nil || c.WaitReady(3*time.Second) != nil {
			c.Close()
			return false
		}
		remote = c
		return true
	}, 40*time.Second, 100*time.Millisecond, "the repeater did not log in to FreeDMR")
	t.Cleanup(remote.Close)
	local := testutils.NewMMDVMClient(mxTGRpt, "", testPassword)
	t.Cleanup(local.Close)
	require.NoError(t, local.Connect(stack.MMDVMAddr))
	require.NoError(t, local.WaitReady(handshakeWait))
	time.Sleep(12 * time.Second)

	stream := uint(0x7E00)
	send := func(from *testutils.MMDVMClient, to *testutils.MMDVMClient, name string) {
		t.Helper()
		stream++
		tx := voicePayload().build(t, call{group: true, dst: 91, src: mxRadio})[0]
		for _, p := range tx.packets(stream) {
			require.NoError(t, from.SendDMRD(p))
			time.Sleep(60 * time.Millisecond)
		}
		got := ofCall(to.Drain(5*time.Second), mxRadio, 91)
		assert.Len(t, got, len(tx.bursts), "%s: every packet of the call arrives", name)
	}
	send(local, remote, "hub to FreeDMR")
	time.Sleep(6 * time.Second)
	crossLiveKeeping(t, local, remote, liveSMS(t, call{dst: liveRemoteRadio, src: mxRadio}), 0x7E81, "SMS from the hub", firstCSBKOnly)
	time.Sleep(6 * time.Second)
	crossLive(t, remote, local, liveSMS(t, call{dst: mxRadio, src: liveRemoteRadio}), 0x7E82, "SMS to the hub")
	time.Sleep(6 * time.Second)
	send(remote, local, "FreeDMR to hub")
	checkForgedPacketsAreDropped(t, stack.OpenBridgeAddr, freeDMRServerID, obPassword, true, local)

	time.Sleep(65 * time.Second)
	send(remote, local, "FreeDMR to hub after a minute of keepalives")
	assert.NotContains(t, logs.String(), "as last KeepAlive was", "FreeDMR stopped sending for lack of keepalives")
}
