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
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
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
// takes a login. Afterwards the goroutines are back to what they were.
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
