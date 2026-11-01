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
	"errors"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
)

func TestPendingSessionsAreCapped(t *testing.T) {
	t.Parallel()
	var s Server
	for i := range maxPending {
		_, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, byte(i>>8), byte(i))}, dmrd.DialectMMDVM)
		if err != nil {
			t.Fatalf("link %d: %v", i, err)
		}
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 1, 0, 0)}, dmrd.DialectMMDVM); !errors.Is(err, errTooManyPending) {
		t.Fatalf("got %v, want errTooManyPending", err)
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, 0, 0)}, dmrd.DialectMMDVM); err != nil {
		t.Fatalf("known address: %v", err)
	}
}

func TestHalfOpenLoginEndsAfterLoginTimeout(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	r := newRig(t, []uint{311860}, withClock(fake))
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTL, ID: 311860})
	r.expect(dmrd.DialectMMDVM, dmrd.CommandRPTACK)
	l, st, _ := r.link(r.s)
	require.NotNil(t, l)
	require.Equal(t, dmrdsession.StateAuth, st)

	fake.Advance(dmrdsession.LoginTimeoutDefault - time.Second)
	r.s.tick(context.Background())
	l, _, _ = r.link(r.s)
	require.NotNil(t, l, "the login should still be open before LoginTimeout")

	fake.Advance(2 * time.Second)
	r.s.tick(context.Background())
	l, _, _ = r.link(r.s)
	require.Nil(t, l, "the half-open login should end after LoginTimeout")
}

func TestConfigAcceptOrRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		forget bool
		want   dmrd.Command
	}{
		{name: "accepted", want: dmrd.CommandRPTACK},
		{name: "refused", forget: true, want: dmrd.CommandMSTNAK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const id = 311861
			r := newDialectRig(t, id)
			read := func() dmrd.Command {
				return dmrd.Classify(r.next("answer"), dmrd.DialectMMDVM)
			}

			r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
			require.Equal(t, dmrd.CommandRPTACK, read())
			token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
			require.NoError(t, err)
			r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
			require.Equal(t, dmrd.CommandRPTACK, read())

			if tc.forget {
				require.NoError(t, r.db.Unscoped().Delete(&models.Repeater{}, id).Error)
			}
			var rptc dmrd.MMDVMConfig
			rptc.Reset()
			rptc.SetID(id)
			require.NoError(t, errors.Join(
				rptc.SetCallsign([]byte("N0CALL")),
				rptc.SetRXFrequency(440000000),
				rptc.SetTXFrequency(440000000),
				rptc.SetPower(10),
				rptc.SetColorCode(1),
				rptc.SetLatitude([]byte("+00.0000")),
				rptc.SetLongitude([]byte("+000.0000")),
				rptc.SetHeight(10),
				rptc.SetSlots(3),
			))
			r.send(rptc[:])
			require.Equal(t, tc.want, read())
			l, st, _ := r.link(r.s)
			_, ok := r.s.connected.Load(id)
			if tc.forget {
				require.Nil(t, l)
				require.False(t, ok)
			} else {
				require.Equal(t, dmrdsession.StateRunning, st)
				require.True(t, ok)
			}
		})
	}
}
