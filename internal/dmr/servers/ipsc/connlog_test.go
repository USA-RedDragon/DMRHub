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
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscsession"
	"github.com/stretchr/testify/require"
)

func newLogServer(t *testing.T, ids ...uint) (*IPSCServer, *logPeer) {
	t.Helper()
	database := testDB(t)
	for _, id := range ids {
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeIPSC}).Error)
	}
	s, srv := newHubServer(t, database)
	key := testKey(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return key, true, nil }
	client, addr := dialServer(t, srv)
	return s, &logPeer{conn: client, addr: addr}
}

type logPeer struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func peerLog(t *testing.T, s *IPSCServer, id uint) []string {
	t.Helper()
	s.peerJobs.Wait()
	s.hub.WaitRepeaterEvents()
	events, err := models.ListRepeaterEvents(s.db, id, time.Time{})
	require.NoError(t, err)
	out := make([]string, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, events[i].Kind)
	}
	return out
}

// TestTheIPSCConnectionLogHasRegistrationsAndTheirEnds registers peers
// and ends their registrations by a timeout, a de-registration, and a
// move to another replica. The log shows each registration, the timeout
// and the de-registration, and nothing for the move.
func TestTheIPSCConnectionLogHasRegistrationsAndTheirEnds(t *testing.T) {
	t.Parallel()
	s, p := newLogServer(t, 7, 8, 9)
	for _, id := range []uint32{7, 8, 9} {
		register(t, s, p.conn, p.addr, id)
	}
	require.NoError(t, s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpDeregisterRequest, 8)), p.addr))
	s.handOverPeer(9, time.Now())
	s.tick(context.Background(), s.now()+ipscsession.TimeoutDefault)
	require.Zero(t, peers(s, 7))
	require.Zero(t, peers(s, 9))

	require.Equal(t, []string{connlog.Connected, connlog.Timeout}, peerLog(t, s, 7))
	require.Equal(t, []string{connlog.Connected, connlog.Closed}, peerLog(t, s, 8))
	require.Equal(t, []string{connlog.Connected}, peerLog(t, s, 9))
}

// TestTheIPSCConnectionLogHasAWrongAuthKey sends packets with a trailer of
// another key, which the log shows as one folded entry.
func TestTheIPSCConnectionLogHasAWrongAuthKey(t *testing.T) {
	t.Parallel()
	s, p := newLogServer(t, 10)
	other := testKey(t)
	other[0] ^= 0xff
	for range 2 {
		_ = s.handlePacket(context.Background(), signWith(t, other, control(t, ipscwire.OpRegisterRequest, 10)), p.addr)
	}
	require.Equal(t, []string{connlog.WrongPassword}, peerLog(t, s, 10))
	events, err := models.ListRepeaterEvents(s.db, 10, time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint(2), events[0].Count)
	require.Equal(t, "127.0.0.1", events[0].Address)
}
