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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// The environment of TestExternalHBlink3: the HBlink3 source directory,
// which holds bridge.py, and the Python that runs it.
const (
	hblink3Env       = "DMRHUB_HBLINK3"
	hblink3PythonEnv = "DMRHUB_HBLINK3_PYTHON"
)

// hblink3NetworkID is the NETWORK_ID of the link of HBlink3 to DMRHub,
// which it sends in the repeater field of each DMRD and DMRHub takes as the
// peer ID.
const hblink3NetworkID = 3000004

const hblink3Config = `[GLOBAL]
PATH: %[1]s/
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
REPORT_PORT: %[6]d
REPORT_CLIENTS: 127.0.0.1

[LOGGER]
LOG_FILE: /dev/null
LOG_HANDLERS: console
LOG_LEVEL: DEBUG
LOG_NAME: HBlink

[ALIASES]
TRY_DOWNLOAD: False
PATH: %[1]s/
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
PORT: %[2]d
NETWORK_ID: %[7]d
PASSPHRASE: %[5]s
TARGET_IP: 127.0.0.1
TARGET_PORT: %[3]d
BOTH_SLOTS: True
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL

[MASTER-1]
MODE: MASTER
ENABLED: True
REPEAT: True
MAX_PEERS: 1
EXPORT_AMBE: False
IP: 127.0.0.1
PORT: %[4]d
PASSPHRASE: %[8]s
GROUP_HANGTIME: 5
USE_ACL: False
REG_ACL: PERMIT:ALL
SUB_ACL: PERMIT:ALL
TGID_TS1_ACL: PERMIT:ALL
TGID_TS2_ACL: PERMIT:ALL
`

const hblink3Rules = `BRIDGES = {
    'TG%[1]d': [
        {'SYSTEM': 'OBP-DMRHUB', 'TS': 1, 'TGID': %[1]d, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': 'MASTER-1', 'TS': 1, 'TGID': %[1]d, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
}
UNIT = ['OBP-DMRHUB', 'MASTER-1']
`

// TestExternalHBlink3 links a real HBlink3 (bridge.py) to the hub over
// OpenBridge, the 73-octet DMRD of version 1, with BOTH_SLOTS so that
// HBlink3 keeps the slot of private calls. Group calls on TG mxTG, private
// voice calls and SMS must cross the link both ways, and the hub must drop
// forged packets on the link. It runs only when DMRHUB_HBLINK3 names the
// HBlink3 source and the process is in a network namespace with only
// loopback, as hack/hblink3-test.sh runs it. DMRHUB_HBLINK3_LOG=1 prints
// the log of HBlink3. Each call waits out the GROUP_HANGTIME of 5 s that
// HBlink3 starts its slots in.
func TestExternalHBlink3(t *testing.T) {
	t.Parallel()
	src := os.Getenv(hblink3Env)
	if src == "" {
		t.Skip("set " + hblink3Env + " to the HBlink3 source and run hack/hblink3-test.sh")
	}
	require.NoError(t, testutils.RequireIsolatedNetwork(), "run hack/hblink3-test.sh, which runs the test in a network namespace")
	python := os.Getenv(hblink3PythonEnv)
	if python == "" {
		python = "python3"
	}

	stack := testutils.SetupIntegrationStack(t, testutils.SQLiteMemoryBackend())
	stack.SeedUser(t, mxRadio, "N0CALL")
	stack.SeedTalkgroup(t, mxTG, "TG91")
	stack.SeedMMDVMRepeater(t, mxTGRpt, mxRadio, testPassword)
	stack.AssignTS1StaticTG(t, mxTGRpt, mxTG)

	obpPort, masterPort, reportPort := freePort(t), freePort(t), freePort(t)
	stack.SeedPeer(t, hblink3NetworkID, mxRadio, obPassword, "127.0.0.1", obpPort, true, true)
	stack.SeedPeerRule(t, hblink3NetworkID, true, 1, 99999999)
	stack.SeedPeerRule(t, hblink3NetworkID, false, 1, 99999999)
	stack.StartServers(t)

	_, hubPort, err := net.SplitHostPort(stack.OpenBridgeAddr)
	require.NoError(t, err)
	port, err := strconv.Atoi(hubPort)
	require.NoError(t, err)
	dir := t.TempDir()
	cfg := fmt.Sprintf(hblink3Config, dir, obpPort, port, masterPort, obPassword, reportPort, hblink3NetworkID, testPassword)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hblink.cfg"), []byte(cfg), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rules.py"), fmt.Appendf(nil, hblink3Rules, mxTG), 0o600))
	for name, body := range map[string]string{"peer_ids.json": `{"rptrs": []}`, "subscriber_ids.json": `{"users": []}`, "talkgroup_ids.json": `{"talkgroups": []}`} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	logs := &lockedLog{}
	cmd := exec.CommandContext(t.Context(), python, "-E", "-s", filepath.Join(src, "bridge.py"), "-c", filepath.Join(dir, "hblink.cfg"), "-r", filepath.Join(dir, "rules.py")) //nolint:gosec
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, logs, logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() || os.Getenv("DMRHUB_HBLINK3_LOG") != "" {
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
	}, 40*time.Second, 100*time.Millisecond, "the hotspot did not log in to HBlink3")
	t.Cleanup(remote.Close)
	local := testutils.NewMMDVMClient(mxTGRpt, "", testPassword)
	t.Cleanup(local.Close)
	require.NoError(t, local.Connect(stack.MMDVMAddr))
	require.NoError(t, local.WaitReady(handshakeWait))
	time.Sleep(6 * time.Second)

	ts2 := liveVoice(t, call{dst: mxRadio, src: liveRemoteRadio})
	ts2.slot2 = true
	for i, c := range []struct {
		from, to *testutils.MMDVMClient
		tx       transmission
		name     string
		hang     bool
	}{
		{local, remote, liveVoice(t, call{group: true, dst: mxTG, src: mxRadio}), "group voice from the hub", false},
		{remote, local, liveVoice(t, call{group: true, dst: mxTG, src: liveRemoteRadio}), "group voice to the hub", false},
		{local, remote, liveVoice(t, call{dst: liveRemoteRadio, src: mxRadio}), "private voice from the hub", false},
		{remote, local, ts2, "private voice on TS2 to the hub", false},
		{local, remote, liveSMS(t, call{dst: liveRemoteRadio, src: mxRadio}), "SMS from the hub", true},
		{remote, local, liveSMS(t, call{dst: mxRadio, src: liveRemoteRadio}), "SMS to the hub", false},
	} {
		if c.hang {
			time.Sleep(6 * time.Second)
		}
		crossLive(t, c.from, c.to, c.tx, uint(0x7E01+i), c.name)
	}

	checkForgedPacketsAreDropped(t, stack.OpenBridgeAddr, hblink3NetworkID, obPassword, false, local)
}
