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

package mmdvm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/require"
)

// configHeld starts the login of the repeater id on the rig up to its
// RPTC, and holds the records queue of the repeater, so that the server
// checks the config and has not accepted it yet. The returned func lets
// the config go on.
func configHeld(t *testing.T, r *dialectRig, id uint32) func() {
	t.Helper()
	d := dmrd.DialectMMDVM
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	k := r.expect(d, dmrd.CommandRPTACK)
	salt, ok := k.LoginSalt()
	require.True(t, ok)
	token, err := dmrdauth.Token(d, salt, []byte(testPassword))
	require.NoError(t, err)
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	r.expect(d, dmrd.CommandRPTACK)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	require.True(t, r.s.records.Do(uint(id), func() { <-release }))
	r.send(rptc(t, d, id))
	return free
}

// heldBurst is a burst of stream to the repeater id, the hub routes it
// while the server checks the config of the repeater.
func heldBurst(id uint32, stream, seq uint) models.Packet {
	return models.Packet{Signature: "DMRD", Repeater: uint(id), Src: 1000001, Dst: 1, GroupCall: true, StreamID: stream, Seq: seq}
}

// deliverHeld hands p to the server as the hub does, and waits for the
// work that it starts.
func deliverHeld(r *dialectRig, id uint32, p models.Packet) {
	r.s.deliver(context.Background(), uint(id), p)
	r.s.egressWG.Wait()
}

// expectBursts reads the packets to the repeater after the RPTACK of its
// config up to a marker that goes out after them, and fails the test
// unless the bursts among them are want, in order.
func expectBursts(t *testing.T, r *dialectRig, want []models.Packet) {
	t.Helper()
	r.expect(dmrd.DialectMMDVM, dmrd.CommandRPTACK)
	r.handled(r.s)
	marker := []byte("marker")
	r.s.outgoingChan <- outgoing{raw: rawPacketFrom(udpEndpoint(r.from), marker)}
	var got []models.Packet
	for {
		p := r.next("marker")
		if string(p) == string(marker) {
			break
		}
		if dmrd.Classify(p, dmrd.DialectMMDVM) != dmrd.CommandDMRD {
			continue
		}
		b, ok := models.UnpackPacket(p)
		require.True(t, ok)
		got = append(got, b)
	}
	require.Len(t, got, len(want), "bursts after the RPTACK")
	for i := range want {
		require.Equal(t, want[i].StreamID, got[i].StreamID, "burst %d", i)
		require.Equal(t, want[i].Seq, got[i].Seq, "burst %d", i)
	}
}

// TestACallInTheConfigOfALoginComesAfterItsRPTACK routes bursts to a
// repeater while the server checks the config of its login, after the hub
// took the repeater, as it does before the RPTACK. The repeater must get
// them after the RPTACK, in order, rather than lose them.
func TestACallInTheConfigOfALoginComesAfterItsRPTACK(t *testing.T) {
	t.Parallel()
	const id = uint32(311970)
	r := newDialectRig(t, uint(id))
	free := configHeld(t, r, id)
	want := []models.Packet{heldBurst(id, 7001, 0), heldBurst(id, 7001, 1), heldBurst(id, 7001, 2)}
	for _, p := range want {
		deliverHeld(r, id, p)
	}
	free()
	expectBursts(t, r, want)
}

// TestAHeldCallKeepsItsStart routes more bursts than the server holds for
// a login. The server must keep the first ones, which hold the header and
// the start of the call that a repeater needs, and drop the rest.
func TestAHeldCallKeepsItsStart(t *testing.T) {
	t.Parallel()
	const id = uint32(311971)
	r := newDialectRig(t, uint(id))
	free := configHeld(t, r, id)
	var want []models.Packet
	for i := range maxHeldPerAddress + 3 {
		p := heldBurst(id, 7002, uint(i))
		if i < maxHeldPerAddress {
			want = append(want, p)
		}
		deliverHeld(r, id, p)
	}
	free()
	expectBursts(t, r, want)
}

// TestAnEndedHeldCallIsNotSent routes a burst of a call to a repeater
// while the server checks its config, and then, a stream idle time later,
// a burst of a newer call. The first call has ended by the RPTACK, so the
// repeater must get only the newer one.
func TestAnEndedHeldCallIsNotSent(t *testing.T) {
	t.Parallel()
	const id = uint32(311972)
	clk := clock.NewFake(time.Now())
	r := newRig(t, []uint{uint(id)}, withClock(clk))
	free := configHeld(t, r, id)
	deliverHeld(r, id, heldBurst(id, 7003, 0))
	clk.Advance(hub.StreamIdle + time.Second)
	newer := heldBurst(id, 7004, 0)
	deliverHeld(r, id, newer)
	free()
	expectBursts(t, r, []models.Packet{newer})
}

// TestAHeldCallOfAnEndedLoginIsDropped routes a burst to a repeater while
// the server checks its config, and the login then ends before the
// RPTACK. The next login of the repeater must not get that burst.
func TestAHeldCallOfAnEndedLoginIsDropped(t *testing.T) {
	t.Parallel()
	const id = uint32(311973)
	r := newDialectRig(t, uint(id))
	free := configHeld(t, r, id)
	deliverHeld(r, id, heldBurst(id, 7005, 0))
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTCL, ID: id})
	free()
	r.handled(r.s)
	r.login(dmrd.DialectMMDVM, id)
	r.quiet(r.s)
}

// TestAHeldCallOfARefusedLoginIsDropped routes a burst to a repeater whose
// config the server refuses, as for a repeater that was deleted during
// its login. The next login of the repeater must not get that burst.
func TestAHeldCallOfARefusedLoginIsDropped(t *testing.T) {
	t.Parallel()
	const id = uint32(311974)
	r := newDialectRig(t, uint(id))
	d := dmrd.DialectMMDVM
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	k := r.expect(d, dmrd.CommandRPTACK)
	salt, ok := k.LoginSalt()
	require.True(t, ok)
	token, err := dmrdauth.Token(d, salt, []byte(testPassword))
	require.NoError(t, err)
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	r.expect(d, dmrd.CommandRPTACK)
	var rpt models.Repeater
	require.NoError(t, r.db.First(&rpt, id).Error)
	require.NoError(t, r.db.Unscoped().Delete(&rpt).Error)
	r.s.mu.Lock()
	r.s.handleLocked(context.Background(), udpEndpoint(r.from), rptc(t, d, id), r.s.clock.Now(), noRecord)
	r.s.deliver(context.Background(), uint(id), heldBurst(id, 7006, 0))
	r.s.mu.Unlock()
	r.s.egressWG.Wait()
	r.expect(d, dmrd.CommandMSTNAK)
	r.handled(r.s)
	require.NoError(t, r.db.Create(&rpt).Error)
	r.login(d, id)
	r.quiet(r.s)
}

// TestACallInTheConfigOfABrowserLoginComesAfterItsRPTACK is
// TestACallInTheConfigOfALoginComesAfterItsRPTACK for a browser hotspot
// that logs in over WebTransport.
func TestACallInTheConfigOfABrowserLoginComesAfterItsRPTACK(t *testing.T) {
	t.Parallel()
	const id = uint32(311860)
	s, cfg := webTransportServer(t)
	s.mu.Lock()
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	s.mu.Unlock()
	sess := dialByHash(t, s, cfg)
	read := func() []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), answerWait)
		defer cancel()
		b, err := sess.ReceiveDatagram(ctx)
		require.NoError(t, err)
		return b
	}
	d := dmrd.DialectMMDVM
	rptl, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: id}).AppendBinary(nil, d)
	require.NoError(t, err)
	require.NoError(t, sess.SendDatagram(rptl))
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(read(), d))
	tok, err := dmrdauth.Token(d, testSalt, []byte(testPassword))
	require.NoError(t, err)
	rptk, err := (&dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: tok}).AppendBinary(nil, d)
	require.NoError(t, err)
	require.NoError(t, sess.SendDatagram(rptk))
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(read(), d))

	var ep endpoint
	s.mu.Lock()
	for _, l := range s.links {
		if l.m.ID() == id && l.ep.transport == webTransportName {
			ep = l.ep
		}
	}
	s.mu.Unlock()
	require.Equal(t, webTransportName, ep.transport, "the session of the hotspot")
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	require.True(t, s.records.Do(uint(id), func() { <-release }))
	s.handleFrom(context.Background(), ep, rptc(t, d, id), s.clock.Now())
	want := heldBurst(id, 7007, 0)
	s.deliver(context.Background(), uint(id), want)
	s.egressWG.Wait()
	free()

	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(read(), d))
	for {
		b := read()
		if dmrd.Classify(b, d) != dmrd.CommandDMRD {
			continue
		}
		got, ok := models.UnpackPacket(b)
		require.True(t, ok)
		require.Equal(t, want.StreamID, got.StreamID)
		return
	}
}
