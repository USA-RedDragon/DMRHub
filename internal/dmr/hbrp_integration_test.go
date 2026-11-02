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
	"sync"
	"sync/atomic"
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
	"gorm.io/gorm"
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
		var rec models.Repeater
		waitFor(t, "the server records a ping of the HBRP repeater", func() bool {
			r, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
			rec = r
			return err == nil && r.PingsReceived != 0
		})
		assert.Equal(t, dmrdsession.StateRunning, hb.State())
		assert.Zero(t, hb.Downs())

		assert.Equal(t, "HBRP", rec.Dialect)
		assert.Equal(t, "H0001HB", rec.Callsign)
		assert.Equal(t, "Berlin", rec.Location)
		assert.NotZero(t, rec.PingsReceived, "the MSTPINGs should be recorded")
		var row models.Repeater
		require.NoError(t, stack.DB.First(&row, 100001).Error)
		assert.Equal(t, "HBRP", row.Dialect)
	})
}

func TestHBRPLoginWithCallsignStartingWithL(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.StartServers(t)

		hb, err := testutils.NewHBRPSessionPeerCalled(100001, testPassword, "LU1ABC")
		require.NoError(t, err)
		require.NoError(t, hb.Connect(stack.MMDVMAddr))
		defer hb.Close()
		require.NoError(t, hb.WaitReady(handshakeWait))

		var row models.Repeater
		waitFor(t, "the server records the callsign of the login", func() bool {
			return stack.DB.First(&row, 100001).Error == nil && row.Callsign == "LU1ABC"
		})
		assert.Equal(t, "LU1ABC", row.Callsign)
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
	return hb, mm
}

// TestHBRPVoiceBothWays passes group and private voice calls between an
// HBRP repeater and an MMDVM repeater in both directions.
func TestHBRPVoiceBothWays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		hb, mm := hbrpPair(t, stack)

		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 7201, false)))
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7201, false)))
		pkts := receiveCall(mm, 1)
		require.NotEmpty(t, pkts, "the MMDVM repeater should get the group call of the HBRP repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
			assert.Equal(t, uint(100002), p.Repeater)
		}

		require.NoError(t, mm.SendDMRD(makeGroupVoicePacket(1000001, 1, 7202, false)))
		require.NoError(t, mm.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7202, false)))
		pkts = hb.Receive(deliverWait, callEnds(1))
		require.NotEmpty(t, pkts, "the HBRP repeater should get the group call of the MMDVM repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.True(t, p.GroupCall)
			assert.Equal(t, uint(100001), p.Repeater)
		}

		require.NoError(t, hb.SendDMRD(makePrivateVoicePacket(100002, 7203)))
		require.NoError(t, hb.SendDMRD(makePrivateVoiceTermPacket(100002, 7203)))
		pkts = receiveCall(mm, 100002)
		require.NotEmpty(t, pkts, "the MMDVM repeater should get the private call of the HBRP repeater")
		for _, p := range pkts {
			assert.Equal(t, uint(100002), p.Dst)
			assert.False(t, p.GroupCall)
		}

		require.NoError(t, mm.SendDMRD(makePrivateVoicePacket(100001, 7204)))
		require.NoError(t, mm.SendDMRD(makePrivateVoiceTermPacket(100001, 7204)))
		pkts = hb.Receive(deliverWait, callEnds(100001))
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
				arrived := testutils.Collect(tc.recv(), deliverWait, gotN[models.Packet](1))
				require.Len(t, arrived, 1, "%s burst %d did not arrive before the next was sent", tc.name, i)
				got := arrived[0]
				assert.Equal(t, sent.DMRData, got.DMRData, "%s burst %d octets", tc.name, i)
				assert.Equal(t, sent.FrameType, got.FrameType, "%s burst %d", tc.name, i)
				assert.Equal(t, sent.DTypeOrVSeq, got.DTypeOrVSeq, "%s burst %d", tc.name, i)
				assert.Equal(t, tc.group, got.GroupCall, "%s burst %d", tc.name, i)
			}
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

		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 7301, false)))
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7301, false)))
		assert.NotEmpty(t, receiveCall(mm, 1), "calls should pass after the RPTO")
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

		waitFor(t, "the HBRP repeater logs in again", func() bool { return hb.Ups() >= 2 })
		require.Equal(t, 2, hb.Ups(), "the HBRP repeater should log in again")
		assert.Equal(t, 1, hb.Downs(), "the MSTCL should end the session once")
		require.NoError(t, mm.WaitReady(handshakeWait))
		waitFor(t, "the KV store records the HBRP dialect of the new session", func() bool {
			rec, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
			return err == nil && rec.Dialect == "HBRP"
		})
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
		waitRecorded(t, stack, 100001, 100002)
		hb.PausePings()

		stack.RestartMMDVM(t, testutils.RestartHandoff)

		require.NoError(t, talker.SendDMRD(makeBurst(1000001, 1, 9201, true, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0)))
		got := hb.Receive(deliverWait, gotN[models.Packet](1))
		require.NotEmpty(t, got, "the listening HBRP repeater got nothing after the handoff")
		assert.Equal(t, uint(9201), got[0].StreamID)
		assert.Equal(t, 1, hb.Ups(), "the listening HBRP repeater got the call only after a new login")

		require.NoError(t, hb.Retarget(second.MMDVMAddr))
		hb.ResumePings()
		waitSubscribed(t, second.Hub, 100001, 1, true)
		pings := func() uint {
			rec, err := servers.MakeKVClient(stack.KV).GetRepeater(context.Background(), 100001)
			if err != nil {
				return 0
			}
			return rec.PingsReceived
		}
		before := pings()
		require.NoError(t, hb.SendDMRD(makeGroupVoicePacket(1000001, 1, 9202, false)))
		require.NoError(t, hb.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 9202, false)))
		waitFor(t, "a ping of the HBRP repeater after its call", func() bool { return pings() > before })
		assert.Equal(t, dmrdsession.StateRunning, hb.State())
		assert.Zero(t, hb.Downs(), "no replica should refuse the HBRP session")
		assert.Equal(t, 1, hb.Ups(), "the HBRP repeater should not log in again")
	})
}

// TestMixedDialectLoginIsRefused starts a login in the MMDVM network
// protocol and answers the salt with an HBRP RPTK. DMRHub must not
// complete the login, and must go on serving other repeaters. The
// database answers the first read of a repeater after slowLookup, as on a
// starved machine, so the salt comes late.
func TestMixedDialectLoginIsRefused(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		slow := slowRepeaterRead(t, stack.DB)
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.StartServers(t)
		slow.Store(true)

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
		ack, err := dmrd.DecodeControl(read(handshakeWait), dmrd.DialectMMDVM)
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

// slowLookup is how long the slow read of slowRepeaterRead takes: longer
// than the second that a test once gave the server to answer an RPTL.
const slowLookup = 2 * time.Second

// slowRepeaterRead makes the first read of the repeaters table on database
// after the returned flag is set take slowLookup. Call it before anything
// else uses database.
func slowRepeaterRead(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	var armed atomic.Bool
	var once sync.Once
	require.NoError(t, database.Callback().Query().Before("gorm:query").Register("slow_repeater_read", func(tx *gorm.DB) {
		if tx.Statement.Table != "repeaters" || !armed.Load() {
			return
		}
		once.Do(func() {
			done := make(chan struct{})
			time.AfterFunc(slowLookup, func() { close(done) })
			<-done
		})
	}))
	return &armed
}
