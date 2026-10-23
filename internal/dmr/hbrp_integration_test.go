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
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHBRPLogin logs in a repeater that speaks HBRP, with hex text IDs,
// MSTACK and the 306-octet RPTC.
func TestHBRPLogin(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.StartServers(t)

		hb, err := testutils.NewHBRPSessionPeer(100001, testPassword)
		require.NoError(t, err)
		require.NoError(t, hb.Connect(stack.MMDVMAddr))
		defer hb.Close()
		require.NoError(t, hb.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		assert.Equal(t, dmrdsession.StateRunning, hb.State())
		assert.Zero(t, hb.Downs())

		rec, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
		require.NoError(t, err)
		assert.Equal(t, "HBRP", rec.Dialect)
		assert.Equal(t, "H0001HB", rec.Callsign)
		assert.Equal(t, "Berlin", rec.Location)
		assert.NotZero(t, rec.PingsReceived, "the MSTPINGs should be recorded")
		var row models.Repeater
		require.NoError(t, stack.DB.First(&row, 100001).Error)
		assert.Equal(t, "HBRP", row.Dialect)
	})
}

// hbrpPair logs in the HBRP repeater 100001 and the MMDVM repeater 100002,
// both on TG1 TS1.
func hbrpPair(t *testing.T, stack *testutils.IntegrationStack) (*testutils.SessionPeer, *testutils.MMDVMClient) {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
	stack.AssignTS1StaticTG(t, 100001, 1)
	stack.AssignTS1StaticTG(t, 100002, 1)
	stack.StartServers(t)

	hb, err := testutils.NewHBRPSessionPeer(100001, testPassword)
	require.NoError(t, err)
	require.NoError(t, hb.Connect(stack.MMDVMAddr))
	t.Cleanup(hb.Close)
	require.NoError(t, hb.WaitReady(handshakeWait))

	mm := testutils.NewMMDVMClient(100002, "", testPassword)
	require.NoError(t, mm.Connect(stack.MMDVMAddr))
	t.Cleanup(mm.Close)
	require.NoError(t, mm.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return hb, mm
}

// TestHBRPVoiceBothWays passes group and private voice calls between an
// HBRP repeater and an MMDVM repeater in both directions.
func TestHBRPVoiceBothWays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		hb, mm := hbrpPair(t, stack)

		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 7201, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7201, false)))
		pkts := mm.Drain(drainWait)
		require.NotEmpty(t, pkts, "the MMDVM repeater should get the group call of the HBRP repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
			assert.Equal(t, uint(100002), p.Repeater)
		}

		time.Sleep(callHoldTime)
		require.NoError(t, mm.SendDMRD(makeGroupVoicePacket(1000001, 1, 7202, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, mm.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7202, false)))
		pkts = hb.Drain(drainWait)
		require.NotEmpty(t, pkts, "the HBRP repeater should get the group call of the MMDVM repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
			assert.Equal(t, uint(100001), p.Repeater)
		}

		time.Sleep(callHoldTime)
		require.NoError(t, hb.SendDMRD(makePrivateVoicePacket(100002, 7203)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, hb.SendDMRD(makePrivateVoiceTermPacket(100002, 7203)))
		pkts = mm.Drain(drainWait)
		require.NotEmpty(t, pkts, "the MMDVM repeater should get the private call of the HBRP repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(100002), p.Dst)
			assert.False(t, p.GroupCall)
		}

		time.Sleep(callHoldTime)
		require.NoError(t, mm.SendDMRD(makePrivateVoicePacket(100001, 7204)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, mm.SendDMRD(makePrivateVoiceTermPacket(100001, 7204)))
		pkts = hb.Drain(drainWait)
		require.NotEmpty(t, pkts, "the HBRP repeater should get the private call of the MMDVM repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(100001), p.Dst)
			assert.False(t, p.GroupCall)
		}
		assert.Zero(t, hb.Downs())
	})
}

// TestHBRPDataBurstsBothWays sends a data call of every burst kind, group
// and private, from the HBRP repeater to the MMDVM repeater and back.
func TestHBRPDataBurstsBothWays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		hb, mm := hbrpPair(t, stack)
		type sender interface{ SendDMRD(models.Packet) error }
		for j, tc := range []struct {
			name  string
			from  sender
			recv  func() <-chan models.Packet
			dst   uint
			group bool
		}{
			{"HBRP to MMDVM group", hb, mm.ReceivedPackets, 1, true},
			{"HBRP to MMDVM private", hb, mm.ReceivedPackets, 100002, false},
			{"MMDVM to HBRP group", mm, hb.ReceivedPackets, 1, true},
			{"MMDVM to HBRP private", mm, hb.ReceivedPackets, 100001, false},
		} {
			for i, sent := range dataCall(1000001, tc.dst, uint(8100+j), tc.group) {
				require.NoError(t, tc.from.SendDMRD(sent))
				select {
				case got := <-tc.recv():
					assert.Equal(t, sent.DMRData, got.DMRData, "%s burst %d octets", tc.name, i)
					assert.Equal(t, sent.FrameType, got.FrameType, "%s burst %d", tc.name, i)
					assert.Equal(t, sent.DTypeOrVSeq, got.DTypeOrVSeq, "%s burst %d", tc.name, i)
					assert.Equal(t, tc.group, got.GroupCall, "%s burst %d", tc.name, i)
				case <-time.After(drainWait):
					t.Fatalf("%s burst %d did not arrive", tc.name, i)
				}
				time.Sleep(100 * time.Millisecond)
			}
			time.Sleep(callHoldTime)
		}
	})
}

// TestHBRPRPTOIsIgnored sends RPTO, which HBRP does not have, from an HBRP
// repeater. DMRHub must not answer it or end the session, and calls must
// still pass.
func TestHBRPRPTOIsIgnored(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		hb, mm := hbrpPair(t, stack)
		bin, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: 100001, Text: []byte("TS1=91;")}).AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		require.NoError(t, hb.SendRaw(bin))
		require.NoError(t, hb.SendRaw([]byte("RPTO000186A1TS1=91;")))
		time.Sleep(settleDuration)

		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 7301, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7301, false)))
		assert.NotEmpty(t, mm.Drain(drainWait), "calls should pass after the RPTO")
		assert.Equal(t, dmrdsession.StateRunning, hb.State())
		assert.Zero(t, hb.Downs())
	})
}

// TestHBRPReconnectsAfterMSTCL stops DMRHub as a lone instance does. The
// HBRP repeater must get the MSTCL in its dialect and log in again.
func TestHBRPReconnectsAfterMSTCL(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		hb, mm := hbrpPair(t, stack)
		stack.RestartMMDVM(t, testutils.RestartClose)

		deadline := time.Now().Add(handshakeWait)
		for hb.Ups() < 2 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		require.Equal(t, 2, hb.Ups(), "the HBRP repeater should log in again")
		assert.Equal(t, 1, hb.Downs(), "the MSTCL should end the session once")
		require.NoError(t, mm.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		rec, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
		require.NoError(t, err)
		assert.Equal(t, "HBRP", rec.Dialect)
	})
}

// TestHBRPHandoff restarts DMRHub as in a handoff while the HBRP repeater
// only listens, and then moves the repeater to a second replica. Both must
// take the session over in HBRP without a new login.
func TestHBRPHandoff(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)

		hb, err := testutils.NewHBRPSessionPeer(100001, testPassword)
		require.NoError(t, err)
		require.NoError(t, hb.Connect(stack.MMDVMAddr))
		defer hb.Close()
		require.NoError(t, hb.WaitReady(handshakeWait))
		talker := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, talker.Connect(stack.MMDVMAddr))
		defer talker.Close()
		require.NoError(t, talker.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		hb.PausePings()

		stack.RestartMMDVM(t, testutils.RestartHandoff)

		start := time.Now()
		require.NoError(t, talker.SendDMRD(makeBurst(1000001, 1, 9201, true, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0)))
		select {
		case got := <-hb.ReceivedPackets():
			assert.Less(t, time.Since(start), time.Second, "the listening HBRP repeater got the call late")
			assert.Equal(t, uint(9201), got.StreamID)
		case <-time.After(3 * time.Second):
			t.Fatal("the listening HBRP repeater got nothing after the handoff")
		}

		require.NoError(t, hb.Retarget(second.MMDVMAddr))
		hb.ResumePings()
		time.Sleep(2 * settleDuration)
		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 9202, false)))
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 9202, false)))
		time.Sleep(settleDuration)
		assert.Equal(t, dmrdsession.StateRunning, hb.State())
		assert.Zero(t, hb.Downs(), "no replica should refuse the HBRP session")
		assert.Equal(t, 1, hb.Ups(), "the HBRP repeater should not log in again")
	})
}

// TestMixedDialectLoginIsRefused starts a login in the MMDVM network
// protocol and answers the salt with an HBRP RPTK. DMRHub must not
// complete the login, and must go on serving other repeaters.
func TestMixedDialectLoginIsRefused(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.StartServers(t)

		raddr, err := net.ResolveUDPAddr("udp", stack.MMDVMAddr)
		require.NoError(t, err)
		conn, err := net.DialUDP("udp", nil, raddr)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		read := func(wait time.Duration) []byte {
			buf := make([]byte, dmrdsession.MaxPacket)
			_ = conn.SetReadDeadline(time.Now().Add(wait))
			n, err := conn.Read(buf)
			if err != nil {
				return nil
			}
			return buf[:n]
		}

		rptl, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: 100001}).AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		_, err = conn.Write(rptl)
		require.NoError(t, err)
		ack, err := dmrd.DecodeControl(read(time.Second), dmrd.DialectMMDVM)
		require.NoError(t, err)
		salt, ok := ack.LoginSalt()
		require.True(t, ok)

		token, err := dmrdauth.Token(dmrd.DialectHBRP, salt, []byte(testPassword))
		require.NoError(t, err)
		rptk, err := (&dmrd.Control{Command: dmrd.CommandRPTK, ID: 100001, Token: token}).AppendBinary(nil, dmrd.DialectHBRP)
		require.NoError(t, err)
		_, err = conn.Write(rptk)
		require.NoError(t, err)
		assert.Nil(t, read(settleDuration), "the HBRP RPTK should get no answer")

		hb, err := testutils.NewHBRPSessionPeer(100002, testPassword)
		require.NoError(t, err)
		require.NoError(t, hb.Connect(stack.MMDVMAddr))
		defer hb.Close()
		require.NoError(t, hb.WaitReady(handshakeWait), "DMRHub should still log in other repeaters")
		assert.False(t, servers.MakeKVClient(stack.KV).RepeaterExists(context.Background(), 100001))
	})
}
