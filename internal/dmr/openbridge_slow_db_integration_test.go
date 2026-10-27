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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const burstPeriod = 60 * time.Millisecond

// dbHold holds every statement of a database while it is held.
type dbHold struct {
	mu      sync.Mutex
	gate    chan struct{}
	held    atomic.Int64
	entered atomic.Int64
}

// holdDatabase returns a dbHold of database that does not hold yet, and
// lets the held statements go when the test ends.
func holdDatabase(t *testing.T, database *gorm.DB) *dbHold {
	t.Helper()
	d := &dbHold{}
	wait := func(*gorm.DB) {
		d.mu.Lock()
		gate := d.gate
		d.mu.Unlock()
		if gate == nil {
			return
		}
		d.entered.Add(1)
		d.held.Add(1)
		<-gate
		d.held.Add(-1)
	}
	cb := database.Callback()
	require.NoError(t, cb.Query().Before("gorm:query").Register("test:hold_query", wait))
	require.NoError(t, cb.Row().Before("gorm:row").Register("test:hold_row", wait))
	require.NoError(t, cb.Raw().Before("gorm:raw").Register("test:hold_raw", wait))
	require.NoError(t, cb.Create().Before("gorm:create").Register("test:hold_create", wait))
	require.NoError(t, cb.Update().Before("gorm:update").Register("test:hold_update", wait))
	require.NoError(t, cb.Delete().Before("gorm:delete").Register("test:hold_delete", wait))
	t.Cleanup(d.release)
	return d
}

// hold makes every statement from now on wait for release.
func (d *dbHold) hold() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gate == nil {
		d.gate = make(chan struct{})
	}
}

// release lets the held statements go, and holds no more.
func (d *dbHold) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gate != nil {
		close(d.gate)
		d.gate = nil
	}
}

func watchStream(got <-chan models.Packet, stream uint, n int) <-chan map[uint]time.Time {
	out := make(chan map[uint]time.Time, 1)
	go func() {
		arrived := map[uint]time.Time{}
		deadline := time.After(30 * time.Second)
		for len(arrived) < n {
			select {
			case p := <-got:
				if p.StreamID == stream {
					arrived[p.Seq] = time.Now()
				}
			case <-deadline:
				out <- arrived
				return
			}
		}
		out <- arrived
	}()
	return out
}

// allBurstsArrive sends 8 bursts of stream and requires each target to get
// all of them.
func allBurstsArrive(t *testing.T, stream uint, send func(uint) []time.Time, targets ...<-chan models.Packet) {
	t.Helper()
	worstBursts(t, stream, 8, send, targets...)
}

func worstBursts(t *testing.T, stream uint, bursts int, send func(uint) []time.Time, targets ...<-chan models.Packet) []time.Duration {
	t.Helper()
	watches := make([]<-chan map[uint]time.Time, len(targets))
	for i, got := range targets {
		watches[i] = watchStream(got, stream, bursts)
	}
	sent := send(stream)
	require.Len(t, sent, bursts)
	worst := make([]time.Duration, len(targets))
	for i, w := range watches {
		arrived := <-w
		require.Len(t, arrived, bursts, "stream %d, target %d", stream, i)
		for seq, at := range arrived {
			worst[i] = max(worst[i], at.Sub(sent[seq]))
		}
	}
	return worst
}

// TestOpenBridgeSlowDatabaseHoldsNoBurst sends calls from an OpenBridge
// peer and from a local repeater while the database holds every
// statement, after the same calls ran once. Every burst must reach the
// other peer and the repeater while the database holds a statement of the
// calls, and the statements must run when the database lets them go.
func TestOpenBridgeSlowDatabaseHoldsNoBurst(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		db := holdDatabase(t, stack.DB)
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.SeedPeer(t, 500001, 1000001, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))

		from := testutils.NewOpenBridgeClient(500001, obPassword)
		t.Cleanup(from.Close)
		require.NoError(t, from.Connect(stack.OpenBridgeAddr))
		to := testutils.NewOpenBridgeClient(500002, obPassword)
		t.Cleanup(to.Close)
		require.NoError(t, to.Connect(stack.OpenBridgeAddr))
		addr := to.LocalAddr()
		stack.SeedPeer(t, 500002, 1000001, obPassword, addr.IP.String(), addr.Port, false, true)
		stack.SeedPeerRule(t, 500002, false, 1, 9999999)
		time.Sleep(settleDuration)

		send := func(fromPeer bool) func(uint) []time.Time {
			return func(stream uint) []time.Time {
				sent := make([]time.Time, 0, 8)
				for i := range 8 {
					p := makeGroupVoicePacket(1000001, 1, stream, false)
					p.Seq = uint(i)
					sent = append(sent, time.Now())
					if fromPeer {
						p.Repeater = 500001
						require.NoError(t, from.SendRawPacket(p))
					} else {
						require.NoError(t, mc.SendDMRD(p))
					}
					time.Sleep(burstPeriod)
				}
				return sent
			}
		}

		allBurstsArrive(t, 8001, send(true), to.ReceivedPackets(), mc.ReceivedPackets())
		allBurstsArrive(t, 8002, send(false), to.ReceivedPackets())
		time.Sleep(200 * time.Millisecond)

		db.hold()
		allBurstsArrive(t, 8003, send(true), to.ReceivedPackets(), mc.ReceivedPackets())
		allBurstsArrive(t, 8004, send(false), to.ReceivedPackets())
		require.Eventually(t, func() bool { return db.held.Load() > 0 }, handshakeWait, 10*time.Millisecond, "the calls ran no statement for the database to hold")
		t.Logf("the calls passed while the database held %d statements", db.held.Load())
		db.release()
		require.Eventually(t, func() bool { return db.held.Load() == 0 }, handshakeWait, 10*time.Millisecond, "the held statements did not run when the database let them go")
	})
}
