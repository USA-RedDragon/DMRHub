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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rotationBursts   = 200
	rotationInterval = 60 * time.Millisecond
	rotationDrain    = 8 * time.Second
)

// rotationCall is a call that the test keeps up through the rotations.
type rotationCall struct {
	src    uint
	stream uint
	tg     uint
	slot   bool
	send   func(models.Packet) error
}

func (c rotationCall) run(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	wg.Add(1)
	go func() {
		defer wg.Done()
		next := time.Now()
		for i := range rotationBursts {
			p := makeGroupVoicePacket(c.src, c.tg, c.stream, c.slot)
			p.Seq = uint(i)
			p.DTypeOrVSeq = dmrconst.VoiceA + uint(i%6)
			if i%6 == 0 {
				p.FrameType = dmrconst.FrameVoiceSync
			}
			if i == rotationBursts-1 {
				p = makeGroupVoiceTermPacket(c.src, c.tg, c.stream, c.slot)
				p.Seq = uint(i)
			}
			if err := c.send(p); err != nil {
				t.Errorf("stream %d burst %d: %v", c.stream, i, err)
			}
			next = next.Add(rotationInterval)
			time.Sleep(time.Until(next))
		}
	}()
}

// heard counts the bursts that a listener got of each stream, by sequence.
type heard map[uint]map[uint]int

func (h heard) add(stream, seq uint) {
	if h[stream] == nil {
		h[stream] = map[uint]int{}
	}
	h[stream][seq]++
}

// check reports the bursts of stream that the listener missed or got more
// than once, of the rotationBursts of a call.
func (h heard) check(t *testing.T, who string, stream uint) {
	t.Helper()
	h.checkN(t, who, stream, rotationBursts)
}

// checkN is check for a call of n bursts.
func (h heard) checkN(t *testing.T, who string, stream uint, n int) {
	t.Helper()
	var lost, twice []uint
	for i := range uint(n) {
		switch h[stream][i] {
		case 0:
			lost = append(lost, i)
		case 1:
		default:
			twice = append(twice, i)
		}
	}
	assert.Empty(t, lost, "%s lost bursts of stream %d", who, stream)
	assert.Empty(t, twice, "%s got bursts of stream %d more than once", who, stream)
}

func heardMMDVM(pkts []models.Packet) heard {
	h := heard{}
	for _, p := range pkts {
		h.add(p.StreamID, p.Seq)
	}
	return h
}

// TestRotationMidCall runs two replicas behind UDPFronts, as the pods of
// DMRHub behind Envoy Gateway, with MMDVM repeaters and an OpenBridge peer
// on them, and two calls in progress. It then rolls the
// replicas as Kubernetes does: a new replica starts, the first one stops,
// and then the second. No repeater or peer may log in again, and every
// listener must get every burst of both calls once.
func TestRotationMidCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedUser(t, 1000002, "N0CALM")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		for _, id := range []uint{100001, 100002, 100003} {
			stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		}
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS2StaticTG(t, 100002, 2)
		stack.AssignTS1StaticTG(t, 100003, 1)
		stack.AssignTS2StaticTG(t, 100003, 2)

		regA := testutils.Register(t, stack.Hub, stack.KV)
		b := stack.SpawnFullReplica(t)
		regB := testutils.Register(t, b.Hub, stack.KV)
		stack.StartServers(t)
		a := stack.Replica()

		ob := testutils.NewOpenBridgeClient(500002, obPassword)
		t.Cleanup(ob.Close)
		require.NoError(t, ob.Connect(stack.OpenBridgeAddr))
		obAddr := ob.LocalAddr()
		stack.SeedPeer(t, 500002, 1000001, obPassword, obAddr.IP.String(), obAddr.Port, true, true)
		stack.SeedPeerRule(t, 500002, false, 1000001, 1000002)

		fronts := testutils.Fronts{MMDVM: testutils.NewUDPFront(t)}
		fronts.Add(t, a)
		peer := func(id uint32, retry, timeout time.Duration) *testutils.SessionPeer {
			p, err := testutils.NewSessionPeerTimed(id, testPassword, "", retry, timeout)
			require.NoError(t, err)
			require.NoError(t, p.Connect(fronts.MMDVM.Addr()))
			t.Cleanup(p.Close)
			require.NoError(t, p.WaitReady(handshakeWait))
			return p
		}
		talkerA := peer(100001, 200*time.Millisecond, 5*time.Second)
		slow := peer(100003, 3*time.Second, 15*time.Second)
		fronts.Add(t, b)
		talkerB := peer(100002, 200*time.Millisecond, 5*time.Second)
		time.Sleep(settleDuration)

		one := rotationCall{src: 1000001, stream: 8101, tg: 1, slot: false, send: talkerB.SendDMRD}
		two := rotationCall{src: 1000002, stream: 8102, tg: 2, slot: true, send: talkerA.SendDMRD}
		var calls sync.WaitGroup
		start := time.Now()
		one.run(t, &calls)
		two.run(t, &calls)

		time.Sleep(1500 * time.Millisecond)
		c := stack.SpawnFullReplica(t)
		regC := testutils.Register(t, c.Hub, stack.KV)
		fronts.Add(t, c)
		_ = regC
		t.Logf("%s: the first replica stops", time.Since(start).Round(time.Millisecond))
		a.Terminate(t, regA, fronts, rotationDrain)
		t.Logf("%s: the first replica stopped, the second stops", time.Since(start).Round(time.Millisecond))
		b.Terminate(t, regB, fronts, rotationDrain)
		t.Logf("%s: the second replica stopped", time.Since(start).Round(time.Millisecond))
		calls.Wait()
		time.Sleep(drainWait)

		for _, p := range []struct {
			name string
			p    *testutils.SessionPeer
		}{{"repeater 100001", talkerA}, {"repeater 100002", talkerB}, {"repeater 100003", slow}} {
			assert.Zero(t, p.p.Downs(), "%s lost its session", p.name)
			assert.Equal(t, 1, p.p.Ups(), "%s logged in again", p.name)
		}

		heardMMDVM(talkerA.Drain(settleDuration)).check(t, "repeater 100001", one.stream)
		heardMMDVM(talkerB.Drain(settleDuration)).check(t, "repeater 100002", two.stream)
		slowHeard := heardMMDVM(slow.Drain(settleDuration))
		slowHeard.check(t, "repeater 100003", one.stream)
		slowHeard.check(t, "repeater 100003", two.stream)

		obHeard := heard{}
		for _, br := range ob.DrainBridges(settleDuration) {
			obHeard.add(uint(br.Data.Stream), uint(br.Data.Seq))
		}
		obHeard.check(t, "the OpenBridge peer", one.stream)
		obHeard.check(t, "the OpenBridge peer", two.stream)
		t.Logf("moves: MMDVM %d", fronts.MMDVM.Moves())
	})
}
