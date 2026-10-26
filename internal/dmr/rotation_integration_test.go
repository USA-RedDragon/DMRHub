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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
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

// moveWindow is a move of a client, at which it may get a packet again: the
// replica that it left sent the packet in the last slack before the move,
// and the replica that it moved to sends it again, since the load
// balancer may have lost it. A copy is in the window when the copy before
// it came at or after from and the copy at or after at.
type moveWindow struct {
	from, at time.Time
}

// windowsAt returns the move windows of a client whose sessions at the
// load balancer ended at the times in ended, where the replica that it
// moved to sends again what the old one sent in the last slack.
func windowsAt(ended []time.Time, slack time.Duration) []moveWindow {
	out := make([]moveWindow, 0, len(ended))
	for _, at := range ended {
		out = append(out, moveWindow{from: at.Add(-slack), at: at})
	}
	return out
}

// arrival is a packet of a call that a client got: its place in the call
// and when it came.
type arrival struct {
	n  uint
	at time.Time
}

// arrivalsOf returns the packets of stream in heard, by their sequence
// numbers.
func arrivalsOf(heard []testutils.Heard, stream uint) []arrival {
	var out []arrival
	for _, h := range heard {
		if h.Packet.StreamID == stream {
			out = append(out, arrival{n: h.Packet.Seq, at: h.At})
		}
	}
	return out
}

// firstArrivalsOf returns the first arrival of each packet of stream in
// heard, as a client that drops copies keeps them.
func firstArrivalsOf(heard []testutils.Heard, stream uint) []arrival {
	seen := map[uint]bool{}
	var out []arrival
	for _, a := range arrivalsOf(heard, stream) {
		if !seen[a.n] {
			seen[a.n] = true
			out = append(out, a)
		}
	}
	return out
}

// windowOf returns the last of windows that holds a copy of a packet that
// came at the time at, when the copy before it came at prev, or -1.
func windowOf(windows []moveWindow, prev, at time.Time) int {
	for i := len(windows) - 1; i >= 0; i-- {
		if !prev.Before(windows[i].from) && !at.Before(windows[i].at) {
			return i
		}
	}
	return -1
}

// checkCall checks the packets that a client got of a call of n packets:
// it must get each, in order, and a packet again only in one of windows,
// where the replica that the client moved to sends again, once and in
// order, what the client may have missed.
func checkCall(t *testing.T, who string, got []arrival, n int, windows []moveWindow) {
	t.Helper()
	checkArrivals(t, who, got, n, windows, arrivalRules{})
}

// checkLastCopies is checkCall for an MMDVM repeater, which drops a copy of
// the last packet that it got and plays a copy of an earlier one as audio:
// a copy must come right after the packet that it repeats.
func checkLastCopies(t *testing.T, who string, got []arrival, n int, windows []moveWindow) {
	t.Helper()
	checkArrivals(t, who, got, n, windows, arrivalRules{lastOnly: true})
}

// checkResent is checkCall for a browser hotspot, which may get a burst
// after later ones once it began to move: a load balancer that ends its
// session, or moves the QUIC path of it to another replica, loses bursts
// that the old replica sent before it knew, while later ones get through
// on the new path before the drain notice does, and the replica that the
// hotspot moves to hands the lost ones over only after the move.
func checkResent(t *testing.T, who string, got []arrival, n int, windows []moveWindow) {
	t.Helper()
	checkArrivals(t, who, got, n, windows, arrivalRules{resends: true})
}

// arrivalRules are the exceptions that checkArrivals makes: resends lets a
// packet come after later ones in a move window, and lastOnly lets only a
// copy of the packet that came last come again.
type arrivalRules struct {
	resends, lastOnly bool
}

func checkArrivals(t *testing.T, who string, got []arrival, n int, windows []moveWindow, rules arrivalRules) {
	t.Helper()
	last := map[uint]time.Time{}
	resent := make([]bool, len(windows))
	var next uint
	prev := -1
	var outside, notLast, late, back []uint
	for _, a := range got {
		at, copied := last[a.n]
		last[a.n] = a.at
		switch {
		case copied:
			w := windowOf(windows, at, a.at)
			switch {
			case w < 0:
				outside = append(outside, a.n)
			case rules.lastOnly && int(a.n) != prev:
				notLast = append(notLast, a.n)
			case int(a.n) <= prev && resent[w]:
				back = append(back, a.n)
			case int(a.n) <= prev:
				resent[w] = true
			case int(a.n) != prev+1:
				back = append(back, a.n)
			}
		case a.n < next && (!rules.resends || !slices.ContainsFunc(windows, func(w moveWindow) bool { return !a.at.Before(w.at) })):
			late = append(late, a.n)
		case a.n < next:
		default:
			next = a.n + 1
		}
		prev = int(a.n)
	}
	var lost []uint
	for i := range uint(n) {
		if _, ok := last[i]; !ok {
			lost = append(lost, i)
		}
	}
	assert.Empty(t, lost, "%s lost packets", who)
	assert.Empty(t, outside, "%s got packets again outside a move window", who)
	assert.Empty(t, notLast, "%s got copies of packets other than the last one", who)
	assert.Empty(t, late, "%s got packets after later ones", who)
	assert.Empty(t, back, "%s got copies out of order", who)
	if bad := slices.Concat(lost, outside, notLast, late, back); len(bad) > 0 {
		logArrivals(t, who, got, windows, slices.Min(bad), slices.Max(bad))
	}
}

// checkCopies checks that a client that drops copies got a packet again
// only in one of windows.
func checkCopies(t *testing.T, who string, got []arrival, windows []moveWindow) {
	t.Helper()
	last := map[uint]time.Time{}
	var outside []uint
	for _, a := range got {
		at, copied := last[a.n]
		last[a.n] = a.at
		if copied && windowOf(windows, at, a.at) < 0 {
			outside = append(outside, a.n)
		}
	}
	assert.Empty(t, outside, "%s got packets again outside a move window", who)
}

// logArrivals logs the move windows and the packets from lo to hi that a
// client got, with a few around them.
func logArrivals(t *testing.T, who string, got []arrival, windows []moveWindow, lo, hi uint) {
	t.Helper()
	for _, w := range windows {
		t.Logf("%s: move window from %s at %s", who, w.from.Format("15:04:05.000000"), w.at.Format("15:04:05.000000"))
	}
	for _, a := range got {
		if a.n+8 >= lo && a.n <= hi+8 {
			t.Logf("%s: packet %d at %s", who, a.n, a.at.Format("15:04:05.000000"))
		}
	}
}

// ipscArrivals returns the packets of the call c that the IPSC peer got,
// by their RTP sequence numbers from the first one, and checks that they
// all have the call control of the call, as an XPR 8400 needs.
func ipscArrivals(t *testing.T, bursts []testutils.IPSCBurst, c rotationCall) []arrival {
	t.Helper()
	var out []arrival
	var control uint32
	var start uint16
	for _, b := range bursts {
		if b.Dst != c.tg {
			continue
		}
		u, err := ipscwire.DecodeUser(b.Data)
		require.NoError(t, err)
		if len(out) == 0 {
			control, start = u.CallControl, u.RTP.Seq
		}
		assert.Equal(t, control, u.CallControl, "a packet of the call to TG %d has another call control", c.tg)
		out = append(out, arrival{n: uint(u.RTP.Seq - start), at: b.At})
	}
	return out
}

// TestRotationMidCall runs two replicas behind UDPFronts, as the pods of
// DMRHub behind Envoy Gateway, with MMDVM repeaters, a browser hotspot over
// WebTransport, an IPSC peer and an OpenBridge peer on them, and two calls
// in progress. It then rolls the replicas as Kubernetes does: a new
// replica starts, the first one stops, and then the second. No repeater or
// peer may log in again, and every listener must get every burst of both
// calls in order. A listener may get a burst twice only at a move, where
// the replica that it moved to sends again what the move may have lost, and
// an MMDVM repeater only its last burst.
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
		b, regB := stack.SpawnRegisteredReplica(t)
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
		c, _ := stack.SpawnRegisteredReplica(t)
		testutils.Fronts{MMDVM: fronts.MMDVM, IPSC: fronts.IPSC}.Add(t, c)
		t.Logf("%s: the first replica stops", time.Since(start).Round(time.Millisecond))
		aStops := time.Now()
		a.Terminate(t, regA, fronts, rotationDrain)
		fronts.WebTransport.Add(t, c.WebTransportAddr)
		t.Logf("%s: the first replica stopped, the second stops", time.Since(start).Round(time.Millisecond))
		bStops := time.Now()
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

		for _, p := range []struct {
			name    string
			p       *testutils.SessionPeer
			streams []uint
		}{{"repeater 100001", talkerA, []uint{one.stream}}, {"repeater 100002", talkerB, []uint{two.stream}}, {"repeater 100003", slow, []uint{one.stream, two.stream}}} {
			heard := p.p.Heard()
			windows := windowsAt(fronts.MMDVM.Ended(p.p.LocalAddr()), mmdvm.MoveSlack)
			for _, stream := range p.streams {
				checkLastCopies(t, fmt.Sprintf("%s on stream %d", p.name, stream), arrivalsOf(heard, stream), rotationBursts, windows)
			}
		}

		moved := browser.Moved()
		require.Len(t, moved, 2)
		browserWindows := append(windowsAt(fronts.WebTransport.Ends(), hub.MoveSlack),
			moveWindow{from: aStops.Truncate(time.Second).Add(-hub.MoveSlack), at: moved[0]},
			moveWindow{from: bStops.Truncate(time.Second).Add(-hub.MoveSlack), at: moved[1]},
		)
		for _, stream := range []uint{one.stream, two.stream} {
			who := fmt.Sprintf("the browser hotspot on stream %d", stream)
			checkCopies(t, who, arrivalsOf(browser.Heard(), stream), browserWindows)
			checkResent(t, who, firstArrivalsOf(browser.Heard(), stream), rotationBursts, browserWindows)
		}

		ipscBursts := ipscPeer.Drain(settleDuration)
		ipscWindows := windowsAt(fronts.IPSC.Ended(ipscPeer.LocalAddr()), hub.MoveSlack)
		for _, c := range []rotationCall{one, two} {
			checkCall(t, fmt.Sprintf("the IPSC peer on TG %d", c.tg), ipscArrivals(t, ipscBursts, c), rotationBursts, ipscWindows)
		}

		bridges := ob.DrainBridges(settleDuration)
		obHeard := make([]testutils.Heard, 0, len(bridges))
		for _, br := range bridges {
			obHeard = append(obHeard, testutils.Heard{Packet: models.Packet{StreamID: uint(br.Data.Stream), Seq: uint(br.Data.Seq)}})
		}
		for _, stream := range []uint{one.stream, two.stream} {
			checkCall(t, fmt.Sprintf("the OpenBridge peer on stream %d", stream), arrivalsOf(obHeard, stream), rotationBursts, nil)
		}
		t.Logf("moves: MMDVM %d, IPSC %d, WebTransport %d", fronts.MMDVM.Moves(), fronts.IPSC.Moves(), fronts.WebTransport.Moves())
	})
}
