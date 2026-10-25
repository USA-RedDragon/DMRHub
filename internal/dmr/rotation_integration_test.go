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
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
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

// heardMMDVM counts the bursts that an MMDVM repeater plays of pkts. A
// replica that a repeater left hands over what it sent in the last
// hub.MoveSlack before the move, which the repeater may have got, and
// MMDVMHost drops a burst that repeats the last one of its call, so the
// listener does too.
func heardMMDVM(pkts []models.Packet) heard {
	h := heard{}
	last := map[uint]uint{}
	for _, p := range pkts {
		if seq, ok := last[p.StreamID]; ok && seq == p.Seq {
			continue
		}
		last[p.StreamID] = p.Seq
		h.add(p.StreamID, p.Seq)
	}
	return h
}

// checkIPSCCalls checks that the IPSC peer got every packet of each call
// in one call, as an XPR 8400 needs it: one call control, and an RTP
// sequence number one more than the one before on each packet. A packet
// that repeats the one before it, as the handover of a move may send, is
// a duplicate that an RTP receiver drops.
func checkIPSCCalls(t *testing.T, bursts []testutils.IPSCBurst, calls ...rotationCall) {
	t.Helper()
	for _, c := range calls {
		var n int
		var control uint32
		var last uint16
		var prev []byte
		for _, b := range bursts {
			if b.Dst != c.tg {
				continue
			}
			if bytes.Equal(b.Data, prev) {
				continue
			}
			prev = b.Data
			u, err := ipscwire.DecodeUser(b.Data)
			require.NoError(t, err)
			if n > 0 {
				assert.Equal(t, control, u.CallControl, "packet %d of the call to TG %d has another call control", n, c.tg)
				assert.Equal(t, last+1, u.RTP.Seq, "packet %d of the call to TG %d skips in the RTP sequence", n, c.tg)
			}
			control, last = u.CallControl, u.RTP.Seq
			n++
		}
		assert.Equal(t, rotationBursts, n, "the IPSC peer got %d packets of the call to TG %d", n, c.tg)
	}
}

// TestRotationMidCall runs two replicas behind UDPFronts, as the pods of
// DMRHub behind Envoy Gateway, with MMDVM repeaters, a browser hotspot over
// WebTransport, an IPSC peer and an OpenBridge peer on them, and two calls
// in progress. It then rolls the replicas as Kubernetes does: a new
// replica starts, the first one stops, and then the second. No repeater or
// peer may log in again, and every listener must get every burst of both
// calls once.
func TestRotationMidCall(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedUser(t, 1000002, "N0CALM")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		for _, id := range []uint{100001, 100002, 100003, 100004} {
			stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		}
		stack.AssignTS1StaticTG(t, 100004, 1)
		stack.AssignTS2StaticTG(t, 100004, 2)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS2StaticTG(t, 100002, 2)
		stack.AssignTS1StaticTG(t, 100003, 1)
		stack.AssignTS2StaticTG(t, 100003, 2)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.AssignTS2StaticTG(t, 200001, 2)

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

		fronts := testutils.Fronts{MMDVM: testutils.NewUDPFront(t), IPSC: testutils.NewUDPFront(t), WebTransport: testutils.NewUDPFront(t)}
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
		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		require.NoError(t, browser.Connect(fronts.WebTransport.Addr()))
		t.Cleanup(browser.Close)
		require.NoError(t, browser.WaitReady(handshakeWait))
		ipscPeer := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ipscPeer.Connect(fronts.IPSC.Addr()))
		t.Cleanup(ipscPeer.Close)
		require.NoError(t, ipscPeer.WaitReady(handshakeWait))
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
		testutils.Fronts{MMDVM: fronts.MMDVM, IPSC: fronts.IPSC}.Add(t, c)
		_ = regC
		t.Logf("%s: the first replica stops", time.Since(start).Round(time.Millisecond))
		a.Terminate(t, regA, fronts, rotationDrain)
		fronts.WebTransport.Add(t, c.WebTransportAddr)
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
		assert.Zero(t, ipscPeer.Downs(), "the IPSC peer lost its registration")
		assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
		assert.Equal(t, 1, browser.Ups(), "the browser hotspot logged in again")
		assert.Equal(t, 2, browser.Moves(), "the browser hotspot did not move at each rotation")

		heardMMDVM(talkerA.Drain(settleDuration)).check(t, "repeater 100001", one.stream)
		heardMMDVM(talkerB.Drain(settleDuration)).check(t, "repeater 100002", two.stream)
		slowHeard := heardMMDVM(slow.Drain(settleDuration))
		slowHeard.check(t, "repeater 100003", one.stream)
		slowHeard.check(t, "repeater 100003", two.stream)
		browserHeard := heardMMDVM(browser.Drain(settleDuration))
		browserHeard.check(t, "the browser hotspot", one.stream)
		browserHeard.check(t, "the browser hotspot", two.stream)

		checkIPSCCalls(t, ipscPeer.Drain(settleDuration), one, two)

		obHeard := heard{}
		for _, br := range ob.DrainBridges(settleDuration) {
			obHeard.add(uint(br.Data.Stream), uint(br.Data.Seq))
		}
		obHeard.check(t, "the OpenBridge peer", one.stream)
		obHeard.check(t, "the OpenBridge peer", two.stream)
		t.Logf("moves: MMDVM %d, IPSC %d, WebTransport %d", fronts.MMDVM.Moves(), fronts.IPSC.Moves(), fronts.WebTransport.Moves())
	})
}
