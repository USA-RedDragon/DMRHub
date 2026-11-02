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

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/stretchr/testify/require"
)

// stoppedRig returns a rig whose server stopped, with a probe transport
// that keeps what the server writes, and the endpoint of a repeater on it.
func stoppedRig(t *testing.T) (*dialectRig, *recordProbe, endpoint) {
	t.Helper()
	r := newDialectRig(t, 990360)
	probe := &recordProbe{out: make(chan []byte, 16)}
	r.s.transports.Store(probeTransport, probe)
	require.NoError(t, r.s.Stop(context.Background()))
	return r, probe, endpoint{transport: probeTransport, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}}
}

// tookNothing fails the test when the stopped server of r answered, or
// started a session or a lookup for, the packets that it was given.
func tookNothing(t *testing.T, r *dialectRig, probe *recordProbe, what string) {
	t.Helper()
	r.handled(r.s)
	select {
	case pkt := <-probe.out:
		t.Fatalf("expected no outgoing packet for %s during shutdown, got %q", what, pkt)
	default:
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	require.Empty(t, r.s.links, "a session for %s during shutdown", what)
	require.Empty(t, r.s.resolving, "a lookup for %s during shutdown", what)
}

// TestHandleFromRejectsWhenStopped is a regression test for the shutdown
// race condition. Before the fix, a repeater could receive MSTCL during
// shutdown, disconnect, and immediately send a new RPTL login that would be
// accepted because the packet handler had no stopped-flag check.
func TestHandleFromRejectsWhenStopped(t *testing.T) {
	t.Parallel()
	r, probe, from := stoppedRig(t)

	repeaterID := uint32(990360)
	rptlPacket := make([]byte, 8)
	copy(rptlPacket[:4], []byte(dmrconst.CommandRPTL))
	binary.BigEndian.PutUint32(rptlPacket[4:], repeaterID)

	r.s.handleFrom(context.Background(), from, rptlPacket, r.s.clock.Now())
	tookNothing(t, r, probe, "RPTL")
	require.False(t, r.kv.RepeaterExists(context.Background(), uint(repeaterID)), "expected no repeater to be stored in KV during shutdown")
}

// TestHandleFromRejectsAllCommandsWhenStopped verifies that all command
// types are rejected once the server stopped, not just RPTL.
func TestHandleFromRejectsAllCommandsWhenStopped(t *testing.T) {
	t.Parallel()
	r, probe, from := stoppedRig(t)
	repeaterID := uint32(990360)

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
			t.Parallel()
			r.s.handleFrom(context.Background(), from, tc.data, r.s.clock.Now())
			tookNothing(t, r, probe, tc.name)
		})
	}
}
