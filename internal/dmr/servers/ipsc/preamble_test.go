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

package ipsc

import (
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSMSPeer(t *testing.T) (*IPSCServer, *net.UDPConn) {
	t.Helper()
	s, srv := newTestServerWithUDP(t)
	t.Cleanup(s.pacer.stop)
	c, a := dialServer(t, srv)
	register(t, s, c, a, smsPeer)
	return s, c
}

func readUsers(t *testing.T, c *net.UDPConn, n int) ([]ipscwire.User, [][]byte) {
	t.Helper()
	users := make([]ipscwire.User, 0, n)
	bodies := make([][]byte, 0, n)
	for range n {
		body, ok := ipscauth.NewKey(testKey(t)).Verify(readUDP(t, c))
		require.True(t, ok)
		users = append(users, decodeUserT(t, body))
		bodies = append(bodies, body)
	}
	return users, bodies
}

func assertOneCall(t *testing.T, users []ipscwire.User) {
	t.Helper()
	for i, u := range users {
		assert.Equal(t, users[0].CallControl, u.CallControl, "call control of packet %d", i)
		assert.Equal(t, users[0].CallSeq, u.CallSeq, "call sequence number of packet %d", i)
		assert.Equal(t, i == 0, u.RTP.Marker, "RTP marker of packet %d", i)
	}
}

// TestMMDVMHostSMSGoesToIPSCAsOneCall sends an SMS with preambles to an IPSC
// peer as MMDVMHost sends it, each CSBK and the data header on a new
// stream. An XPR 8400 sends the preambles and the data of an SMS as one
// call, with one call control and call sequence number, and the end flag
// and RTP payload type 0x5E on the last block only (testdata/xpr8400.txt
// "datacall"; ipsc2hbrp-rig cap/preamble.txt, call control 0x400).
func TestMMDVMHostSMSGoesToIPSCAsOneCall(t *testing.T) {
	t.Parallel()
	s, c := newSMSPeer(t)
	pkts := mmdvmHostSMS(t, 1, 5, "one call")
	sendAll(s, pkts)

	users, _ := readUsers(t, c, len(pkts))
	assertOneCall(t, users)
	for i, u := range users {
		last := i == len(users)-1
		assert.Equal(t, last, u.Info.End(), "end flag of packet %d", i)
		want := byte(rtpPayloadType)
		if last {
			want = rtpPayloadTypeEnd
		}
		assert.Equal(t, want, u.RTP.PayloadType, "RTP payload type of packet %d", i)
	}
	expectNoUDPFor(t, c, preambleGap+200*time.Millisecond)
}

// TestPreamblesWithNoDataEndWithASyncLostPacket sends preambles on their own
// streams with no data after them. An XPR 8400 ends such a call with a
// 34-octet packet of burst type 0x13 with the end flag and RTP payload type
// 0x5E, and the timeslot in bit 7 of octet 31 (ipsc2hbrp-rig
// cap/preamble.txt, call controls 0x934 and 0x3290: "13800000").
func TestPreamblesWithNoDataEndWithASyncLostPacket(t *testing.T) {
	t.Parallel()
	s, c := newSMSPeer(t)
	for i, raw := range preambleBursts(t, 3, 2) {
		pkt := smsPacketOf(uint(20+i), raw)
		pkt.Slot = true
		s.sendTo(smsPeer, pkt)
	}

	users, bodies := readUsers(t, c, 4)
	assertOneCall(t, users)
	end := users[3]
	assert.Equal(t, ipscwire.BurstSyncLost, end.Burst)
	assert.True(t, end.Info.End())
	assert.Equal(t, enums.Slot2, end.Info.Slot())
	assert.Equal(t, byte(rtpPayloadTypeEnd), end.RTP.PayloadType)
	assert.Equal(t, []byte{0x13, 0x80, 0x00, 0x00}, bodies[3][30:])
	expectNoUDPFor(t, c, preambleGap+200*time.Millisecond)
}

// TestADataCallWithoutItsLastBlockEnds sends a data call whose last block
// never comes. It ends with the 0x13 packet that an XPR 8400 sends when the
// signal ends, as ipsc2hbrp does.
func TestADataCallWithoutItsLastBlockEnds(t *testing.T) {
	t.Parallel()
	s, c := newSMSPeer(t)
	pkts := mmdvmHostSMS(t, 30, 2, "no last block")
	pkts = pkts[:len(pkts)-1]
	sendAll(s, pkts)

	users, _ := readUsers(t, c, len(pkts)+1)
	assertOneCall(t, users)
	for i, u := range users[:len(pkts)] {
		assert.False(t, u.Info.End(), "end flag of packet %d", i)
	}
	end := users[len(pkts)]
	assert.Equal(t, ipscwire.BurstSyncLost, end.Burst)
	assert.True(t, end.Info.End())
	assert.Equal(t, byte(rtpPayloadTypeEnd), end.RTP.PayloadType)
}

// TestALateDataHeaderStartsAnotherCall sends a data header from the source
// of a preamble after preambleGap, and one from another source at once.
// Neither is the data that the preamble announced.
func TestALateDataHeaderStartsAnotherCall(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	pre := smsPacketOf(40, preambleBursts(t, 1, 1)[0])
	header := smsPacketOf(41, smsBursts(t, "x")[0])
	other := header
	other.StreamID, other.Src = 42, smsSource+1

	first := decodeUserT(t, tr.TranslateToIPSC(pre)[0])
	got := decodeUserT(t, tr.TranslateToIPSC(other)[0])
	assert.NotEqual(t, first.CallControl, got.CallControl, "a header from another source joined the preamble")
	time.Sleep(preambleGap + 50*time.Millisecond)
	got = decodeUserT(t, tr.TranslateToIPSC(header)[0])
	assert.NotEqual(t, first.CallControl, got.CallControl, "a late header joined the preamble")
}

// TestEachCallHasItsOwnCallSeq sends a voice call and two data calls to a
// peer. An XPR 8400 keeps the call sequence number in octet 5 for every
// packet of a call and gives each call the next one (testdata/xpr8400.txt;
// ipsc2hbrp-rig cap/data34u.txt and cap/preamble.txt).
func TestEachCallHasItsOwnCallSeq(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	seqs := map[uint8]bool{}
	voice := make([]models.Packet, 0, 3)
	for i, v := range []struct {
		frame dmrconst.FrameType
		value uint
	}{{dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader)}, {dmrconst.FrameVoiceSync, 0}, {dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC)}} {
		p := makeTestMMDVMPacket(true, false, v.frame, v.value)
		p.StreamID, p.Seq = 50, uint(i)
		if v.frame == dmrconst.FrameVoiceSync {
			p.DMRData = makeVoiceDMRData(true)
		}
		voice = append(voice, p)
	}
	calls := [][]models.Packet{voice, mmdvmHostSMS(t, 60, 0, "a"), mmdvmHostSMS(t, 70, 0, "b")}
	for n, call := range calls {
		var seq uint8
		for i, pkt := range call {
			for _, f := range tr.TranslateToIPSC(pkt) {
				u := decodeUserT(t, f)
				if i == 0 {
					seq = u.CallSeq
				}
				assert.Equal(t, seq, u.CallSeq, "call %d packet %d", n, i)
			}
		}
		assert.False(t, seqs[seq], "call %d has the call sequence number of an earlier call", n)
		seqs[seq] = true
	}
}

func expectNoUDPFor(t *testing.T, conn *net.UDPConn, d time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(d)))
	buf := make([]byte, 1500)
	if n, _, err := conn.ReadFromUDP(buf); err == nil {
		t.Fatalf("expected no packet, got %x", buf[:n])
	}
}
