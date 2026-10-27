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

package manager_test

import (
	"context"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	password = "s3cret"
	ipscKey  = "0000000000000000000000000000000000003039"
	wait     = 5 * time.Second
)

// TestRestartsUnderTrafficLeakNothing restarts the MMDVM server and turns
// OpenBridge off and on, again and again, while an IPSC peer streams a
// call that the hub routes to the MMDVM repeater. The IPSC repeater is
// linked to the talkgroup of the call from the start, so that the call
// adds no subscription of its own. Each new MMDVM server
// takes a login. OpenBridge binds port 0 each time it comes back on, since
// any socket on the host may take a fixed port while it is off. Afterwards
// the goroutines are back to what they were.
func TestRestartsUnderTrafficLeakNothing(t *testing.T) { //nolint:paralleltest // counts the goroutines of the process
	l := testutils.SetupLiveStack(t)
	l.SeedUser(t, 1000001, "N0CALL")
	l.SeedTalkgroup(t, 1, "TG1")
	l.SeedMMDVMRepeater(t, 100001, 1000001, password)
	l.SeedIPSCRepeater(t, 200001, 1000001, ipscKey)
	l.AssignTS1StaticTG(t, 100001, 1)
	require.NoError(t, l.DB.Model(&models.Repeater{}).Where("id = ?", 200001).UpdateColumn("ts1_dynamic_talkgroup_id", 1).Error)

	ic := testutils.NewIPSCClient(200001, ipscKey)
	require.NoError(t, ic.Connect(l.Addr(t, manager.IPSC)))
	defer ic.Close()
	require.NoError(t, ic.WaitReady(wait))
	mc := testutils.NewMMDVMClient(100001, "", password)
	require.NoError(t, mc.Connect(l.Addr(t, manager.MMDVM)))
	require.NoError(t, mc.WaitReady(wait))
	time.Sleep(500 * time.Millisecond)

	before := goroutinesByCreator()

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for stream := uint32(9000); !stop.Load(); stream++ {
			_ = ic.SendGroupVoice(1000001, 1, false, stream)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	received := 0
	for i := range 5 {
		next := *l.Config
		next.DMR.MMDVM.Port = testutils.FreeUDPPort(t)
		next.DMR.OpenBridge.Enabled = i%2 == 1
		next.DMR.OpenBridge.Port = 0
		require.Empty(t, l.Manager.Apply(&next))
		*l.Config = next
		mc.Close()
		mc = testutils.NewMMDVMClient(100001, "", password)
		require.NoError(t, mc.Connect(l.Addr(t, manager.MMDVM)))
		require.NoError(t, mc.WaitReady(wait), "login %d", i)
		received += len(mc.Drain(300 * time.Millisecond))
	}
	stop.Store(true)
	wg.Wait()
	assert.Positive(t, received, "the MMDVM repeater must get the call between restarts")

	next := *l.Config
	next.DMR.OpenBridge.Enabled = true
	require.Empty(t, l.Manager.Apply(&next))
	*l.Config = next

	var grown map[string]int
	assert.Eventually(t, func() bool {
		grown = map[string]int{}
		for creator, n := range goroutinesByCreator() {
			if n > before[creator] {
				grown[creator] = n - before[creator]
			}
		}
		return len(grown) == 0
	}, 10*time.Second, 50*time.Millisecond)
	assert.Empty(t, grown, "goroutines left over after the restarts, by the function that started them")
	mc.Close()
}

// goroutinesByCreator counts the goroutines of DMRHub by the function that
// started them.
func goroutinesByCreator() map[string]int {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	out := map[string]int{}
	for _, line := range strings.Split(string(buf), "\n") {
		creator, ok := strings.CutPrefix(line, "created by ")
		if !ok || !strings.Contains(creator, "USA-RedDragon/DMRHub/") {
			continue
		}
		creator, _, _ = strings.Cut(creator, " in goroutine")
		out[creator]++
	}
	return out
}

func TestStopEndsEveryServerAndApplyAfterStopDoesNothing(t *testing.T) {
	t.Parallel()
	l := testutils.SetupLiveStack(t)
	l.Manager.Stop(context.Background())
	for _, k := range []manager.Kind{manager.MMDVM, manager.IPSC, manager.OpenBridge} {
		assert.Nil(t, l.Manager.Server(k), "%s", k)
	}
	next := *l.Config
	next.DMR.MMDVM.Port = testutils.FreeUDPPort(t)
	assert.Empty(t, l.Manager.Apply(&next))
	assert.Nil(t, l.Manager.Server(manager.MMDVM))
}

func webTransportAddr(t *testing.T, m *manager.Manager) net.Addr {
	t.Helper()
	s, ok := m.Server(manager.MMDVM).(*mmdvm.Server)
	require.True(t, ok)
	return s.WebTransportAddr()
}

func webTransportPort(t *testing.T, m *manager.Manager) int {
	t.Helper()
	a, ok := webTransportAddr(t, m).(*net.UDPAddr)
	require.True(t, ok, "the WebTransport listener is not running")
	return a.Port
}

func TestApplyTurnsWebTransportOnMovesItAndTurnsItOff(t *testing.T) {
	t.Parallel()
	l := testutils.SetupLiveStack(t)
	require.Nil(t, webTransportAddr(t, l.Manager))

	next := *l.Config
	next.DMR.MMDVM.WebTransport = config.MMDVMWebTransport{Enabled: true, Port: testutils.FreeUDPPort(t)}
	require.Empty(t, l.Manager.Apply(&next))
	require.NotNil(t, webTransportAddr(t, l.Manager), "turning WebTransport on starts its listener")
	assert.Equal(t, next.DMR.MMDVM.WebTransport.Port, webTransportPort(t, l.Manager))

	moved := next
	moved.DMR.MMDVM.WebTransport.Port = testutils.FreeUDPPort(t)
	require.Empty(t, l.Manager.Apply(&moved))
	assert.Equal(t, moved.DMR.MMDVM.WebTransport.Port, webTransportPort(t, l.Manager), "a new port moves the listener")

	both := moved
	both.DMR.MMDVM.Port = testutils.FreeUDPPort(t)
	require.Empty(t, l.Manager.Apply(&both), "a new MMDVM port with the same WebTransport port restarts the server")
	assert.Equal(t, moved.DMR.MMDVM.WebTransport.Port, webTransportPort(t, l.Manager))

	off := both
	off.DMR.MMDVM.WebTransport.Enabled = false
	require.Empty(t, l.Manager.Apply(&off))
	assert.Nil(t, webTransportAddr(t, l.Manager), "turning WebTransport off stops its listener")
}

// TestApplyRestartsMMDVMForNewAllowedOrigins changes the allowed origins of
// WebTransport. The listener takes them when it is made, so a change makes
// a new server, and the same list keeps the one that runs.
func TestApplyRestartsMMDVMForNewAllowedOrigins(t *testing.T) {
	t.Parallel()
	l := testutils.SetupLiveStack(t)
	next := *l.Config
	next.DMR.MMDVM.WebTransport = config.MMDVMWebTransport{Enabled: true, Port: testutils.FreeUDPPort(t)}
	require.Empty(t, l.Manager.Apply(&next))
	before := l.Manager.Server(manager.MMDVM)

	same := next
	same.DMR.MMDVM.WebTransport.AllowedOrigins = nil
	require.Empty(t, l.Manager.Apply(&same))
	assert.Same(t, before, l.Manager.Server(manager.MMDVM), "the same settings keep the server")

	limited := next
	limited.DMR.MMDVM.WebTransport.AllowedOrigins = []string{"https://radio.example"}
	require.Empty(t, l.Manager.Apply(&limited))
	after := l.Manager.Server(manager.MMDVM)
	assert.NotSame(t, before, after, "new allowed origins make a new server")

	again := limited
	again.DMR.MMDVM.WebTransport.AllowedOrigins = []string{"https://radio.example"}
	require.Empty(t, l.Manager.Apply(&again))
	assert.Same(t, after, l.Manager.Server(manager.MMDVM), "an equal list keeps the server")
}

// TestApplyRestartsIPSCForNewHangHolds changes each IPSC hang hold. The
// IPSC server takes its holds when it is made, so each change makes a new
// one, and the same holds keep the one that runs.
func TestApplyRestartsIPSCForNewHangHolds(t *testing.T) {
	t.Parallel()
	l := testutils.SetupLiveStack(t)
	before := l.Manager.Server(manager.IPSC)
	require.NotNil(t, before)

	same := *l.Config
	require.Empty(t, l.Manager.Apply(&same))
	assert.Same(t, before, l.Manager.Server(manager.IPSC), "the same settings restart the server")

	call := same
	call.DMR.IPSC.CallHangHold = 0
	require.Empty(t, l.Manager.Apply(&call))
	afterCall := l.Manager.Server(manager.IPSC)
	assert.NotSame(t, before, afterCall, "a new call hang hold keeps the old server")

	data := call
	data.DMR.IPSC.DataHangHold = 0
	require.Empty(t, l.Manager.Apply(&data))
	assert.NotSame(t, afterCall, l.Manager.Server(manager.IPSC), "a new data hang hold keeps the old server")
}
