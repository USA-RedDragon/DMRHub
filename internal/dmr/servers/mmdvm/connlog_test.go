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
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
)

func logOf(t *testing.T, r *dialectRig, s *Server, id uint) []models.RepeaterEvent {
	t.Helper()
	r.handled(s)
	s.hub.WaitRepeaterEvents()
	events, err := models.ListRepeaterEvents(r.db, id, time.Time{})
	require.NoError(t, err)
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events
}

func kinds(events []models.RepeaterEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// TestTheConnectionLogHasALoginAWrongPasswordAndALogout logs a hotspot in,
// logs it out with RPTCL, and tries a wrong password three times. The log
// holds the login with the address and the software, the logout, and one
// folded entry for the wrong passwords.
func TestTheConnectionLogHasALoginAWrongPasswordAndALogout(t *testing.T) {
	t.Parallel()
	const id = 3000301
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTCL, ID: id})
	r.handled(r.s)
	for range 3 {
		r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
		r.expect(dmrd.DialectMMDVM, dmrd.CommandRPTACK)
		token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte("wrong"))
		require.NoError(t, err)
		r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
		r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTNAK)
	}

	events := logOf(t, r, r.s, id)
	require.Equal(t, []string{connlog.Connected, connlog.Closed, connlog.WrongPassword}, kinds(events))
	require.Equal(t, "127.0.0.1", events[0].Address)
	require.Equal(t, uint(3), events[2].Count)
	require.Equal(t, "127.0.0.1", events[2].Address)
}

// TestATimeoutIsLoggedAndAMoveIsNot lets the session of a hotspot time
// out twice on a test clock: once with no other replica, which the log
// shows as a lost connection, and once with a ping of the hotspot that
// another replica stored at the time of the timeout, as after a move,
// which it does not log. The ticker of the server may end the session as
// soon as the clock moves, so the other ping is stored first.
func TestATimeoutIsLoggedAndAMoveIsNot(t *testing.T) {
	t.Parallel()
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost", true: "moved"}[moved], func(t *testing.T) {
			t.Parallel()
			const id = 3000302
			fake := clock.NewFake(time.Now())
			r := newRig(t, []uint{id}, withClock(fake))
			r.login(dmrd.DialectMMDVM, id)
			r.handled(r.s)
			end := dmrdsession.TimeoutDefault + time.Second
			if moved {
				rec, err := r.kv.GetRepeater(context.Background(), id)
				require.NoError(t, err)
				rec.LastPing = fake.Now().Add(end)
				r.kv.StoreRepeater(context.Background(), id, rec)
			}
			fake.Advance(end)
			r.s.tick(context.Background())
			require.False(t, serves(r.s, id))

			want := []string{connlog.Connected, connlog.Timeout}
			if moved {
				want = want[:1]
			}
			require.Equal(t, want, kinds(logOf(t, r, r.s, id)))
		})
	}
}

// TestARotatedPasswordAndAnotherLoginAreLogged ends the session of a
// hotspot for a new password, logs it in again, and then logs it in from
// another IP address while that session runs. The log shows the rotation,
// both logins, and the push off from the other address.
func TestARotatedPasswordAndAnotherLoginAreLogged(t *testing.T) {
	t.Parallel()
	const id = 3000303
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	r.s.endSessions(context.Background(), id, connlog.PasswordRotated)
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTCL)

	other, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	at := udpEndpoint(*other.LocalAddr().(*net.UDPAddr)) //nolint:forcetypeassert
	send := func(p []byte) {
		r.s.handleFrom(context.Background(), at, p, r.s.clock.Now())
		r.handled(r.s)
	}
	control := func(c dmrd.Control) {
		p, err := c.AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		send(p)
	}
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	control(dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
	require.NoError(t, err)
	control(dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	send(rptc(t, dmrd.DialectMMDVM, id))

	events := logOf(t, r, r.s, id)
	require.Equal(t, []string{connlog.Connected, connlog.PasswordRotated, connlog.Connected, connlog.PushedOff, connlog.Connected}, kinds(events))
	require.Equal(t, "127.0.0.2", events[3].Address)
	require.Equal(t, "127.0.0.2", events[4].Address)
}

// TestALoginAfterABrowserSessionIsNotLoggedAsAPushOff stores the KV record
// of a running session of a browser hotspot, whose address is no IP
// address, and logs the hotspot in over UDP. The log has the login only.
func TestALoginAfterABrowserSessionIsNotLoggedAsAPushOff(t *testing.T) {
	t.Parallel()
	const id = 3000304
	r := newDialectRig(t, id)
	r.kv.StoreRepeater(context.Background(), id, models.Repeater{Connection: models.RepeaterStateConnected, IP: "webtransport/198.51.100.24:4433#1", LastPing: r.s.clock.Now()})
	r.login(dmrd.DialectMMDVM, id)
	require.Equal(t, []string{connlog.Connected}, kinds(logOf(t, r, r.s, id)))
}
