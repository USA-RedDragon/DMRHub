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
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/puzpuzpuz/xsync/v4"
)

// TestHandlePacketRejectsWhenStopped is a regression test for the shutdown race
// condition. Before the fix, a repeater could receive MSTCL during shutdown,
// disconnect, and immediately send a new RPTL login that would be accepted
// because handlePacket had no stopped-flag check.
func TestHandlePacketRejectsWhenStopped(t *testing.T) {
	t.Parallel()

	kvStore := newMockKV()
	kvClient := servers.MakeKVClient(kvStore)

	s := Server{
		kvClient:     kvClient,
		connected:    xsync.NewMap[uint, struct{}](),
		incomingChan: make(chan models.RawDMRPacket, channelBufferSize),
		outgoingChan: make(chan models.RawDMRPacket, channelBufferSize),
	}

	// Simulate server shutdown
	s.stopped.Store(true)

	remoteAddr := net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	// Build a valid RPTL (login) packet: "RPTL" + 4-byte repeater ID
	repeaterID := uint32(311860)
	rptlPacket := make([]byte, 8)
	copy(rptlPacket[:4], []byte(dmrconst.CommandRPTL))
	binary.BigEndian.PutUint32(rptlPacket[4:], repeaterID)

	// handlePacket should return immediately without processing
	s.handlePacket(context.Background(), remoteAddr, rptlPacket)

	// Verify no response was queued (no RPTACK/MSTNAK sent)
	select {
	case pkt := <-s.outgoingChan:
		t.Fatalf("expected no outgoing packet during shutdown, got %v", pkt)
	case <-time.After(100 * time.Millisecond):
		// Good — nothing was sent
	}

	// Also verify no repeater was stored in KV
	exists := kvClient.RepeaterExists(context.Background(), uint(repeaterID))
	if exists {
		t.Fatal("expected no repeater to be stored in KV during shutdown")
	}
}

// TestHandlePacketRejectsAllCommandsWhenStopped verifies that all command types
// are rejected once the stopped flag is set, not just RPTL.
func TestHandlePacketRejectsAllCommandsWhenStopped(t *testing.T) {
	t.Parallel()

	kvStore := newMockKV()
	kvClient := servers.MakeKVClient(kvStore)

	s := Server{
		kvClient:     kvClient,
		connected:    xsync.NewMap[uint, struct{}](),
		incomingChan: make(chan models.RawDMRPacket, channelBufferSize),
		outgoingChan: make(chan models.RawDMRPacket, channelBufferSize),
	}

	s.stopped.Store(true)

	remoteAddr := net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	repeaterID := uint32(311860)

	commands := []struct {
		name string
		data []byte
	}{
		{
			name: "RPTL",
			data: func() []byte {
				pkt := make([]byte, 8)
				copy(pkt[:4], []byte(dmrconst.CommandRPTL))
				binary.BigEndian.PutUint32(pkt[4:], repeaterID)
				return pkt
			}(),
		},
		{
			name: "RPTK",
			data: func() []byte {
				pkt := make([]byte, 40)
				copy(pkt[:4], []byte(dmrconst.CommandRPTK))
				binary.BigEndian.PutUint32(pkt[4:], repeaterID)
				return pkt
			}(),
		},
		{
			name: "RPTC",
			data: func() []byte {
				pkt := make([]byte, 8)
				copy(pkt[:4], []byte(dmrconst.CommandRPTC))
				binary.BigEndian.PutUint32(pkt[4:], repeaterID)
				return pkt
			}(),
		},
		{
			name: "RPTPING",
			data: func() []byte {
				pkt := make([]byte, 11)
				copy(pkt[:7], []byte(dmrconst.CommandRPTPING))
				binary.BigEndian.PutUint32(pkt[7:], repeaterID)
				return pkt
			}(),
		},
		{
			name: "DMRD",
			data: func() []byte {
				pkt := make([]byte, 55)
				copy(pkt[:4], []byte(dmrconst.CommandDMRD))
				return pkt
			}(),
		},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			s.handlePacket(context.Background(), remoteAddr, tc.data)

			// Verify nothing was queued
			select {
			case pkt := <-s.outgoingChan:
				t.Fatalf("expected no outgoing packet for %s during shutdown, got %v", tc.name, pkt)
			case <-time.After(50 * time.Millisecond):
				// Good
			}
		})
	}
}
