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
	"os"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const droidStarReconnects = 5

// TestExternalDroidStar runs a desktop DroidStar (nostar/DroidStar with the
// DROIDSTAR_E2E_AUTOCONNECT hook) as an MMDVM client of the hub. It must
// log in, show the call info of group voice calls on both slots, log in
// again after an MSTCL and after an MSTNAK, each about 10 s later, and keep
// its session through a handoff to a new server, which resumes it at the
// first packet that the hub has for it. It runs
// only when DMRHUB_DROIDSTAR names the binary and the process is in a
// network namespace with only loopback, as hack/droidstar-test.sh runs it.
func TestExternalDroidStar(t *testing.T) {
	t.Parallel()
	bin := os.Getenv(testutils.DroidStarEnv)
	if bin == "" {
		t.Skip("set " + testutils.DroidStarEnv + " to a DroidStar binary and run hack/droidstar-test.sh")
	}
	require.NoError(t, testutils.RequireIsolatedNetwork(), "run hack/droidstar-test.sh, which runs the test in a network namespace")

	stack := testutils.SetupIntegrationStack(t, testutils.SQLiteMemoryBackend())
	cfg := &testutils.DroidStarConfig{
		Binary: bin, Home: t.TempDir(), Callsign: "KI5VMF", DMRID: anyTone, ESSID: 1,
		Password: "passw0rd", Options: "TS1=91;TS2=9;", TalkGroup: 91,
	}
	hotspot := uint(cfg.HotspotID())
	stack.SeedUser(t, anyTone, "KI5VMF")
	stack.SeedUser(t, mxRadio, "N0CALL")
	stack.SeedTalkgroup(t, 91, "TG91")
	stack.SeedTalkgroup(t, 9, "TG9")
	stack.SeedMMDVMRepeater(t, hotspot, anyTone, cfg.Password)
	stack.SeedMMDVMRepeater(t, mxSrcRpt, mxRadio, testPassword)
	stack.AssignTS1StaticTG(t, hotspot, 91)
	stack.AssignTS2StaticTG(t, hotspot, 9)
	stack.StartServers(t)
	cfg.Master = stack.MMDVMAddr

	start := time.Now()
	ds, err := testutils.StartDroidStar(cfg)
	require.NoError(t, err)
	defer func() {
		ds.Stop()
		if t.Failed() {
			t.Log(ds.Log())
		}
	}()
	require.True(t, waitLogin(t, stack, hotspot, start, 30*time.Second), "DroidStar did not log in")
	t.Logf("login after %s", time.Since(start).Round(time.Millisecond))

	r := &droidStarRun{stack: stack, ds: ds, hotspot: hotspot, stream: 0x1D00}
	r.run(t, "voice", r.hearCalls)
	r.run(t, "reconnect-after-MSTCL", func(t *testing.T) {
		r.reconnects(t, "MSTCL", "Received MSTCL from master", testutils.RestartClose)
	})
	r.run(t, "reconnect-after-MSTNAK", func(t *testing.T) {
		r.reconnects(t, "MSTNAK", "Received MSTNAK from master", testutils.RestartLost)
	})
	r.run(t, "handoff-keeps-session", r.handoffs)
}

type droidStarRun struct {
	stack   *testutils.IntegrationStack
	ds      *testutils.DroidStar
	hotspot uint
	stream  uint
}

func (r *droidStarRun) run(t *testing.T, name string, f func(t *testing.T)) {
	t.Run(name, f)
}

func (r *droidStarRun) hearCalls(t *testing.T) {
	t.Helper()
	src := testutils.NewMMDVMClient(mxSrcRpt, "", testPassword)
	defer src.Close()
	require.NoError(t, src.Connect(r.stack.MMDVMAddr))
	require.NoError(t, src.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	for _, c := range []struct {
		tg    uint
		slot2 bool
	}{{91, false}, {9, true}} {
		r.stream++
		from := r.ds.Lines()
		tx := voicePayload().build(t, call{group: true, dst: uint32(c.tg), src: mxRadio})[0]
		tx.slot2 = c.slot2
		sendPackets(t, src.SendDMRD, tx.packets(r.stream))
		want := fmt.Sprintf("New DMR stream from  %d  to  %d", mxRadio, c.tg)
		_, ok := r.ds.WaitLine(from, want, 5*time.Second)
		assert.True(t, ok, "DroidStar did not show %q", want)
		time.Sleep(time.Second)
	}
}

func (r *droidStarRun) reconnects(t *testing.T, name, line string, mode testutils.RestartMode) {
	t.Helper()
	times := make([]time.Duration, 0, droidStarReconnects)
	for i := range droidStarReconnects {
		from := r.ds.Lines()
		at := time.Now()
		r.stack.RestartMMDVM(t, mode)
		_, ok := r.ds.WaitLine(from, line, 20*time.Second)
		require.True(t, ok, "try %d: DroidStar did not log %q", i, line)
		require.True(t, waitLogin(t, r.stack, r.hotspot, at, 40*time.Second), "try %d: DroidStar did not log in again", i)
		d := time.Since(at)
		times = append(times, d)
		t.Logf("%s try %d: logged in again after %s", name, i, d.Round(time.Millisecond))
		assert.Less(t, d, 30*time.Second)
	}
	t.Logf("%s reconnect times: %v", name, times)
	r.hearCalls(t)
}

func (r *droidStarRun) handoffs(t *testing.T) {
	t.Helper()
	gaps := make([]time.Duration, 0, droidStarReconnects)
	for i := range droidStarReconnects {
		from := r.ds.Lines()
		at := time.Now()
		r.stack.RestartMMDVM(t, testutils.RestartHandoff)
		gap, ok := firstCallHeard(t, r.stack, r.ds, &r.stream, at, 20*time.Second)
		require.True(t, ok, "try %d: DroidStar heard no call after the handoff", i)
		gaps = append(gaps, gap)
		t.Logf("handoff try %d: first call heard after %s", i, gap.Round(time.Millisecond))
		_, told := r.ds.WaitLine(from, "from master - reconnecting", 0)
		assert.False(t, told, "try %d: DroidStar was told to reconnect after a handoff", i)
		assert.False(t, waitLogin(t, r.stack, r.hotspot, at, 0), "try %d: DroidStar logged in again after a handoff", i)
	}
	t.Logf("handoff times to the first call heard: %v", gaps)
}

func firstCallHeard(t *testing.T, stack *testutils.IntegrationStack, ds *testutils.DroidStar, stream *uint, at time.Time, timeout time.Duration) (time.Duration, bool) {
	t.Helper()
	src := testutils.NewMMDVMClient(mxSrcRpt, "", testPassword)
	defer src.Close()
	require.NoError(t, src.Connect(stack.MMDVMAddr))
	require.NoError(t, src.WaitReady(handshakeWait))
	want := fmt.Sprintf("New DMR stream from  %d  to  91", mxRadio)
	for time.Since(at) < timeout {
		*stream++
		from := ds.Lines()
		tx := voicePayload().build(t, call{group: true, dst: 91, src: mxRadio})[0]
		sendPackets(t, src.SendDMRD, tx.packets(*stream))
		if _, ok := ds.WaitLine(from, want, time.Second); ok {
			return time.Since(at), true
		}
	}
	return 0, false
}

func waitLogin(t *testing.T, stack *testutils.IntegrationStack, id uint, after time.Time, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		r, err := models.FindRepeaterByID(stack.DB, id)
		if err == nil && r.Connected.After(after) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
