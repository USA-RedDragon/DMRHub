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
	"encoding/binary"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAMovedIPSCCallKeepsItsStream moves an IPSC peer to another replica
// in the middle of a call that it sends. The handover carries the state of
// the call, so the replica that the peer moved to goes on with the same
// stream ID and sequence numbers, and the hub and the call tracker see one
// call.
func TestAMovedIPSCCallKeepsItsStream(t *testing.T) {
	t.Parallel()
	const peer = 99999
	old := &IPSCServer{clock: clock.Real(), localID: 1, translator: NewIPSCTranslator(1)}
	moved := &IPSCServer{clock: clock.Real(), localID: 1, translator: NewIPSCTranslator(1), pacer: newPacer(clock.Real(), func(slotKey, []byte) {}, nil)}

	header := makeTestIPSCPacket(0x80, ipscBurstVoiceHead, true, false)
	before := old.translator.TranslateToMMDVM(header[0], header)
	for i := range 4 {
		p := ipscVoiceTestPacket(header, 0xAAAA, i)
		before = append(before, old.translator.TranslateToMMDVM(p[0], p)...)
	}
	require.NotEmpty(t, before)

	report := handoverReport{Ingress: old.ingressOf(peer)}
	moved.applyReport(peer, &report, true)

	var after []models.Packet
	for i := 4; i < 10; i++ {
		p := ipscVoiceTestPacket(header, 0xAAAA, i)
		after = append(after, moved.translator.TranslateToMMDVM(p[0], p)...)
	}
	require.NotEmpty(t, after)
	for _, p := range after {
		assert.Equal(t, before[0].StreamID, p.StreamID, "the stream of the call changed at the move")
	}
	assert.Equal(t, (before[len(before)-1].Seq+1)&0xff, after[0].Seq, "the sequence numbers of the call started again at the move")
}

// TestTwoIPSCServersNumberTheirStreamsApart starts two IPSC translators,
// as two replicas do, and checks that they give the first calls that they
// hear different stream IDs, so that a call that moves keeps a stream ID
// that the other replica does not give another call.
func TestTwoIPSCServersNumberTheirStreamsApart(t *testing.T) {
	t.Parallel()
	header := makeTestIPSCPacket(0x80, ipscBurstVoiceHead, true, false)
	a := NewIPSCTranslator(1).TranslateToMMDVM(header[0], header)
	b := NewIPSCTranslator(1).TranslateToMMDVM(header[0], header)
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.NotEqual(t, a[0].StreamID, b[0].StreamID)
}

// TestTwoPeersWithOneCallControlKeepTheirCallsApart sends a call from two
// peers that use the same call control, as two repeaters that number
// their calls alike do. Each call has its own stream.
func TestTwoPeersWithOneCallControlKeepTheirCallsApart(t *testing.T) {
	t.Parallel()
	tr := NewIPSCTranslator(1)
	a := makeTestIPSCPacket(0x80, ipscBurstVoiceHead, true, false)
	b := makeTestIPSCPacket(0x80, ipscBurstVoiceHead, true, false)
	binary.BigEndian.PutUint32(b[1:5], 88888)
	pa := tr.TranslateToMMDVM(a[0], a)
	pb := tr.TranslateToMMDVM(b[0], b)
	require.Len(t, pa, 1)
	require.Len(t, pb, 1)
	assert.NotEqual(t, pa[0].StreamID, pb[0].StreamID)
}

// TestABurstBeforeTheEarlyReportKeepsTheStream sends a burst of a call
// from a peer that moved here before the early report of the handover
// came, as a peer that sends right after its registration reply does. The
// burst waits for the report and then goes on with the stream of the call,
// so the call never gets a second stream.
func TestABurstBeforeTheEarlyReportKeepsTheStream(t *testing.T) {
	t.Parallel()
	const peer = 99999
	old := &IPSCServer{clock: clock.Real(), localID: 1, translator: NewIPSCTranslator(1)}
	moved := &IPSCServer{clock: clock.Real(), localID: 1, translator: NewIPSCTranslator(1), pacer: newPacer(clock.Real(), func(slotKey, []byte) {}, nil), moving: map[uint32]*moving{peer: newMoving()}}

	header := makeTestIPSCPacket(0x80, ipscBurstVoiceHead, true, false)
	before := old.translator.TranslateToMMDVM(header[0], header)
	for i := range 4 {
		p := ipscVoiceTestPacket(header, 0xAAAA, i)
		before = append(before, old.translator.TranslateToMMDVM(p[0], p)...)
	}
	require.NotEmpty(t, before)

	next := moved.translator.nextStreamID
	early := ipscVoiceTestPacket(header, 0xAAAA, 4)
	moved.fromPeer(t.Context(), early[0], early, time.Now())
	assert.Equal(t, next, moved.translator.nextStreamID, "a burst before the report started a stream of its own")

	moved.startHandover(peer, time.Now(), &handoverReport{Ingress: old.ingressOf(peer)})
	assert.Equal(t, next, moved.translator.nextStreamID, "the burst before the report started a stream of its own")
	got := moved.translator.ingressCalls(peer)
	require.Len(t, got, 1)
	assert.Equal(t, uint32(before[0].StreamID), got[0].Stream)
	assert.Equal(t, uint8(before[len(before)-1].Seq+2), got[0].Seq, "the held burst went on with the sequence numbers of the call")
}
