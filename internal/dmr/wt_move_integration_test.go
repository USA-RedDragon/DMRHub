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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebTransportMovesFromADrainingReplica drains the replica of a
// browser hotspot behind a UDPFront while a call to its talkgroup goes on.
// The hotspot must move to the other replica with the token of the drain
// notice, with no new login, and get every burst of the call once.
func TestWebTransportMovesFromADrainingReplica(t *testing.T) {
	t.Parallel()
	testWebTransportMove(t, 0)
}

// TestWebTransportMoveWithALateResumeAnswer is
// TestWebTransportMovesFromADrainingReplica with a resume answer that takes
// effect a few pings late, as on a starved machine. The old replica ends
// the session once the new one takes it over and answers those pings with
// MSTNAK, which the browser hotspot must not take for the end of its
// session.
func TestWebTransportMoveWithALateResumeAnswer(t *testing.T) {
	t.Parallel()
	testWebTransportMove(t, 600*time.Millisecond)
}

// testWebTransportMove moves a browser hotspot from a draining replica
// mid-call, with the resume answer late by late.
func testWebTransportMove(t *testing.T, late time.Duration) {
	t.Helper()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100004, 1)
		regA := testutils.Register(t, stack.Hub, stack.KV)
		b, _ := stack.SpawnRegisteredReplica(t)
		stack.StartServers(t)
		require.NotEmpty(t, stack.WebTransportAddr)

		front := testutils.NewUDPFront(t)
		front.Add(t, stack.WebTransportAddr)
		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		browser.SetResumeLate(late)
		require.NoError(t, browser.Connect(front.Addr()))
		t.Cleanup(browser.Close)
		require.NoError(t, browser.WaitReady(handshakeWait))
		front.Add(t, b.WebTransportAddr)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(b.MMDVMAddr))
		t.Cleanup(talker.Close)
		require.NoError(t, talker.WaitReady(handshakeWait))

		const bursts = 80
		var drained time.Time
		next := time.Now()
		for i := range bursts {
			p := makeGroupVoicePacket(1000001, 1, 8201, false)
			p.Seq = uint(i)
			p.DTypeOrVSeq = dmrconst.VoiceA + uint(i%6)
			if i%6 == 0 {
				p.FrameType = dmrconst.FrameVoiceSync
			}
			require.NoError(t, talker.SendDMRD(p))
			if i == 15 {
				drained = time.Now()
				regA.MarkDraining(t.Context())
				front.Drain(stack.WebTransportAddr)
				stack.MMDVMServer.DrainWebTransport(t.Context())
			}
			next = next.Add(rotationInterval)
			time.Sleep(time.Until(next))
		}
		assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
		assert.Equal(t, 1, browser.Ups(), "the browser hotspot logged in again")
		checkMovedBrowser(t, browser, front, drained, 8201, bursts)
	})
}

// checkMovedBrowser checks the call of n bursts on stream that browser got
// while it moved once, at a drain that began at drained, behind front: it
// must keep each burst, in order but for the ones that its replicas send
// again, and get a burst twice only from the drain notice on, or where
// front moved the path of its QUIC connection.
func checkMovedBrowser(t *testing.T, browser *testutils.WebTransportPeer, front *testutils.UDPFront, drained time.Time, stream uint, n int) {
	t.Helper()
	waitFor(t, "the browser hotspot moves", func() bool { return browser.Moves() >= 1 })
	awaitBursts(browser.Heard, n, stream)
	browser.Drain(drainWait)
	moved := browser.Moved()
	require.Len(t, moved, 1, "the browser hotspot did not move")
	windows := append(windowsAt(front.Ends(), hub.MoveSlack), moveWindow{from: drained.Truncate(time.Second).Add(-hub.MoveSlack), at: moved[0]})
	heard := browser.Heard()
	checkCopies(t, "the browser hotspot", arrivalsOf(heard, stream), windows)
	checkResent(t, "the browser hotspot", firstArrivalsOf(heard, stream), n, windows)
}

// TestWebTransportMoveGetsWhatTheLoadBalancerLost drains the replica of a
// browser hotspot while a call to its talkgroup goes on, and the load
// balancer ends the session of the hotspot to that replica at once and
// takes 300 ms to open it a new one, so that the bursts that the draining
// replica sends meanwhile are lost. The replica that the hotspot moves to
// must hand it those bursts, so that it gets every burst of the call once.
func TestWebTransportMoveGetsWhatTheLoadBalancerLost(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100004, 1)
		regA := testutils.Register(t, stack.Hub, stack.KV)
		b, _ := stack.SpawnRegisteredReplica(t)
		stack.StartServers(t)
		require.NotEmpty(t, stack.WebTransportAddr)

		front := testutils.NewUDPFront(t)
		front.Add(t, stack.WebTransportAddr)
		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		require.NoError(t, browser.Connect(front.Addr()))
		t.Cleanup(browser.Close)
		require.NoError(t, browser.WaitReady(handshakeWait))
		front.Add(t, b.WebTransportAddr)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(b.MMDVMAddr))
		t.Cleanup(talker.Close)
		require.NoError(t, talker.WaitReady(handshakeWait))

		const bursts = 80
		var drained time.Time
		next := time.Now()
		for i := range bursts {
			p := makeGroupVoicePacket(1000001, 1, 8201, false)
			p.Seq = uint(i)
			p.DTypeOrVSeq = dmrconst.VoiceA + uint(i%6)
			if i%6 == 0 {
				p.FrameType = dmrconst.FrameVoiceSync
			}
			require.NoError(t, talker.SendDMRD(p))
			if i == 15 {
				drained = time.Now()
				regA.MarkDraining(t.Context())
				stack.MMDVMServer.DrainWebTransport(t.Context())
				front.Cut(stack.WebTransportAddr, 300*time.Millisecond)
			}
			next = next.Add(rotationInterval)
			time.Sleep(time.Until(next))
		}
		checkMovedBrowser(t, browser, front, drained, 8201, bursts)
		assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
		assert.Equal(t, 1, browser.Ups(), "the browser hotspot logged in again")
	})
}

// TestWebTransportMoveWaitsForAStarvedHandover drains the replica of a
// browser hotspot while the load balancer holds the hotspot off the other
// replica, so that a call in that time goes only to the draining replica.
// The handover of the draining replica then comes after handoverWait on
// the test clock of the replicas, as on a starved machine. The replica that
// the hotspot moved to must wait for it while the other replica still
// drains, and the hotspot must get every burst of the call.
func TestWebTransportMoveWaitsForAStarvedHandover(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			checkHeldWebTransportMove(t, be, time.Second, 2*time.Second)
		})
	}
}

// TestWebTransportMoveInTheSecondOfItsLogin drains the replica of a browser
// hotspot in the second of its login, so that the drain notice, which is
// whole seconds, is before the claim of the login. The draining replica
// must still hand the hotspot over, and the hotspot must get every burst of
// a call that went only to the draining replica.
func TestWebTransportMoveInTheSecondOfItsLogin(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			checkHeldWebTransportMove(t, be, 0, 0)
		})
	}
}

// checkHeldWebTransportMove moves a browser hotspot, which logged in
// loggedFor before on the test clock of the replicas, from a draining
// replica that alone gets a call while the load balancer holds the hotspot
// off for 1 s, longer than hub.MoveSlack, so that only the handover holds
// the call. It holds that handover for heldFor of the test clock once it
// came. The hotspot must get every burst of the call.
func checkHeldWebTransportMove(t *testing.T, be testutils.Backend, loggedFor, heldFor time.Duration) {
	t.Helper()
	clk := clock.NewFake(time.Now().Truncate(time.Second).Add(500 * time.Millisecond))
	stack := testutils.SetupIntegrationStack(t, be, testutils.WebTransportOn(), testutils.ClockOn(clk))
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
	stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100002, 1)
	stack.AssignTS1StaticTG(t, 100004, 1)
	regA := testutils.Register(t, stack.Hub, stack.KV)
	held := &heldReport{PubSub: stack.PubSub, topic: "hub:handover:" + models.RepeaterTypeMMDVM + ":100004", release: make(chan struct{})}
	shared := stack.PubSub
	stack.PubSub = held
	b, _ := stack.SpawnRegisteredReplica(t)
	stack.PubSub = shared
	stack.StartServers(t)

	front := testutils.NewUDPFront(t)
	front.Add(t, stack.WebTransportAddr)
	browser, err := testutils.NewWebTransportPeer(100004, testPassword)
	require.NoError(t, err)
	require.NoError(t, browser.Connect(front.Addr()))
	t.Cleanup(browser.Close)
	require.NoError(t, browser.WaitReady(handshakeWait))
	front.Add(t, b.WebTransportAddr)
	listener := testutils.NewMMDVMClient(100002, "", testPassword)
	require.NoError(t, listener.Connect(stack.MMDVMAddr))
	t.Cleanup(listener.Close)
	require.NoError(t, listener.WaitReady(handshakeWait))
	talker := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, talker.Connect(b.MMDVMAddr))
	t.Cleanup(talker.Close)
	require.NoError(t, talker.WaitReady(handshakeWait))
	waitRecorded(t, stack, 100002, 100004)

	const bursts, stream = 12, 8301
	release := make(chan struct{})
	clk.Advance(loggedFor)
	regA.MarkDraining(t.Context())
	front.CutUntil(stack.WebTransportAddr, release)
	stack.MMDVMServer.DrainWebTransport(t.Context())
	for i := range bursts {
		p := makeGroupVoicePacket(1000001, 1, stream, false)
		p.Seq = uint(i)
		if i == bursts-1 {
			p = makeGroupVoiceTermPacket(1000001, 1, stream, false)
			p.Seq = uint(i)
		}
		require.NoError(t, talker.SendDMRD(p))
	}
	require.NotEmpty(t, receiveCall(listener, 1), "the draining replica routed the call")
	clk.Advance(time.Second)
	close(release)
	waitFor(t, "the browser hotspot moves", func() bool { return browser.Moves() == 1 })
	waitFor(t, "the draining replica hands the browser hotspot over", func() bool {
		clk.Advance(10 * time.Millisecond)
		return held.arrived()
	})
	for range heldFor / (100 * time.Millisecond) {
		clk.Advance(100 * time.Millisecond)
	}
	close(held.release)
	waitFor(t, "the browser hotspot gets the call", func() bool {
		clk.Advance(10 * time.Millisecond)
		return len(firstArrivalsOf(browser.Heard(), stream)) >= bursts
	})
	assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
}
