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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	smsSource = 3118601
	smsTarget = 3118602
	smsPeer   = 311860
)

func smsBursts(t *testing.T, body string) []layer2.RawBurst {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: smsSource, Destination: smsTarget, Body: []byte(body)}, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	out, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return out
}

func preambleBursts(t *testing.T, n, follow int) []layer2.RawBurst {
	t.Helper()
	out := make([]layer2.RawBurst, 0, n)
	for i := range n {
		var cs pdu.CSBK
		require.NoError(t, cs.SetPreamble(pdu.PreamblePDU{
			Data:               true,
			CSBKBlocksToFollow: byte(follow + n - 1 - i),
			TargetAddress:      smsTarget,
			SourceAddress:      smsSource,
		}))
		cs.LastBlock = true
		p, err := cs.Encode()
		require.NoError(t, err)
		raw, err := layer2.BuildDataBurst(p, enums.DataTypeCSBK, 1, layer2.SyncSourceMS)
		require.NoError(t, err)
		out = append(out, raw)
	}
	return out
}

func smsPacketOf(stream uint, raw layer2.RawBurst) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         smsSource,
		Dst:         smsTarget,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(raw.SlotType().DataType),
		StreamID:    stream,
		DMRData:     raw,
		BER:         -1,
		RSSI:        -1,
	}
}

// mmdvmHostSMS returns the packets of an SMS with preambles as MMDVMHost
// sends them to the network: each CSBK and the data header start a new
// stream ID, and the data blocks keep the stream ID of their header.
func mmdvmHostSMS(t *testing.T, firstStream uint, preambles int, body string) []models.Packet {
	t.Helper()
	data := smsBursts(t, body)
	out := make([]models.Packet, 0, preambles+len(data))
	stream := firstStream
	for _, raw := range preambleBursts(t, preambles, len(data)) {
		out = append(out, smsPacketOf(stream, raw))
		stream++
	}
	for _, raw := range data {
		out = append(out, smsPacketOf(stream, raw))
	}
	return out
}

func sendAll(s *IPSCServer, pkts []models.Packet) {
	for _, pkt := range pkts {
		s.sendTo(smsPeer, pkt)
	}
}

func assertBackToBack(t *testing.T, sends []hangSend, what string) {
	t.Helper()
	for i := 1; i < len(sends); i++ {
		assert.Less(t, sends[i].at.Sub(sends[i-1].at), 3*burstTime, "%s: gap before packet %d", what, i)
	}
}

func receive(t *testing.T, log *hangLog, n int, within time.Duration) []hangSend {
	t.Helper()
	out := make([]hangSend, 0, n)
	for range n {
		out = append(out, log.next(t, within))
	}
	return out
}

// TestPacerSendsAnMMDVMHostSMSWithoutGaps sends two SMSs with preambles to
// an IPSC peer, as an MMDVMHost source sends them: each preamble CSBK on
// its own stream, then the data on another. The data must follow its
// preambles at once, or the target radio that woke for the preambles
// misses it. The second SMS must wait dataHangHold after the last block of
// the first, not the callHang quiet that ends a call with no terminator.
func TestPacerSendsAnMMDVMHostSMSWithoutGaps(t *testing.T) {
	t.Parallel()
	p, _, log, discarded := newHangPacer(t)
	s := &IPSCServer{pacer: p, localID: 1}
	first := mmdvmHostSMS(t, 1, 5, "first")
	second := mmdvmHostSMS(t, 100, 5, "second")
	sendAll(s, first)
	sendAll(s, second)

	a := receive(t, log, len(first), callHang+time.Second)
	assertBackToBack(t, a, "first SMS")
	b := receive(t, log, len(second), callHang+dataHangHold+time.Second)
	assertBackToBack(t, b, "second SMS")
	gap := b[0].at.Sub(a[len(a)-1].at)
	assert.GreaterOrEqual(t, gap, dataHangHold, "the second SMS went out in the hold of the first")
	assert.Less(t, gap, dataHangHold+3*burstTime, "the second SMS waited past the hold of the first")
	assert.Empty(t, *discarded)
}

// TestPacerKeepsAHeldPreambleRunWhole sends an SMS with 16 preambles, as
// MMDVMHost sends them, to a peer in the call hang of a voice call. The
// preambles and the data wait as one call, so none is discarded for the
// bound on waiting calls, and they go out back to back when the hang ends.
func TestPacerKeepsAHeldPreambleRunWhole(t *testing.T) {
	t.Parallel()
	p, clock, log, discarded := newHangPacer(t)
	s := &IPSCServer{pacer: p, localID: 1}
	k := slotKey{peer: smsPeer, slot: false}
	sendVoiceCall(p, k, 1, 'v')
	receive(t, log, 3, time.Second)

	pkts := mmdvmHostSMS(t, 10, 16, "held")
	sendAll(s, pkts)
	assert.Empty(t, *discarded, "streams of the held SMS that were discarded")
	log.none(t, "the SMS went out in the call hang")
	clock.endHang()
	got := receive(t, log, len(pkts), time.Second)
	assertBackToBack(t, got, "held SMS")
}

// TestPacerSendsAOneStreamSMSWithoutGaps sends an SMS whose preambles and
// data share one stream, as an XPR or ipsc2hbrp sends them, and then a
// CSBK. The CSBK waits dataHangHold after the last block, and nothing
// waits for the callHang quiet.
func TestPacerSendsAOneStreamSMSWithoutGaps(t *testing.T) {
	t.Parallel()
	p, _, log, discarded := newHangPacer(t)
	s := &IPSCServer{pacer: p, localID: 1}
	data := smsBursts(t, "one")
	raws := append(preambleBursts(t, 3, len(data)), data...)
	pkts := make([]models.Packet, 0, len(raws))
	for _, raw := range raws {
		pkts = append(pkts, smsPacketOf(7, raw))
	}
	sendAll(s, pkts)
	csbk := smsPacketOf(8, preambleBursts(t, 1, 0)[0])
	s.sendTo(smsPeer, csbk)

	got := receive(t, log, len(pkts), callHang+time.Second)
	assertBackToBack(t, got, "one-stream SMS")
	next := log.next(t, callHang+dataHangHold+time.Second)
	gap := next.at.Sub(got[len(got)-1].at)
	assert.GreaterOrEqual(t, gap, dataHangHold)
	assert.Less(t, gap, dataHangHold+3*burstTime)
	assert.Empty(t, *discarded)
}
