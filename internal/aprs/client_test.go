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

package aprs_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/aprs/aprstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const line = "N6CALL-9>APZDMH,K6CALL-R*,qAR,K0CALL-10:!3016.03N/09744.59W>"

func start(t *testing.T, srv *aprstest.Server, passcode string) (*aprs.Client, <-chan bool) {
	t.Helper()
	c := aprs.NewClient(aprs.Config{Login: "K0CALL-10", Passcode: passcode, Server: srv.Addr()}, "1.2.3 dirty")
	aprs.SetBackoff(c, 50*time.Millisecond)
	logins := make(chan bool, 16)
	c.OnLogin(func(verified bool) { logins <- verified })
	c.Start(context.Background())
	t.Cleanup(c.Stop)
	return c, logins
}

func reply(t *testing.T, logins <-chan bool) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), aprs.ReadTimeout)
	defer cancel()
	select {
	case verified := <-logins:
		return verified
	case <-ctx.Done():
		t.Fatalf("no login reply within %s, the read timeout of the client", aprs.ReadTimeout)
		return false
	}
}

func lines(t *testing.T, srv *aprstest.Server, n int) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), aprs.ReadTimeout)
	defer cancel()
	return srv.WaitLines(ctx, n)
}

func TestTheClientLogsInAndSends(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c, logins := start(t, srv, "12345")
	require.True(t, reply(t, logins))
	assert.Equal(t, []string{"user K0CALL-10 pass 12345 vers DMRHub 1.2.3-dirty"}, srv.Logins())
	require.NoError(t, c.Send(line))
	assert.Equal(t, []string{line}, lines(t, srv, 1))
}

func TestAnUnverifiedClientSendsNothing(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c, logins := start(t, srv, "-1")
	require.False(t, reply(t, logins))
	assert.Len(t, srv.Logins(), 1)
	assert.False(t, c.LoggedIn())
	assert.ErrorIs(t, c.Send(line), aprs.ErrNotLoggedIn)
}

func TestTheClientLogsInAgainAfterTheServerDrops(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c, logins := start(t, srv, "12345")
	require.True(t, reply(t, logins))
	srv.Drop()
	require.True(t, reply(t, logins))
	assert.Len(t, srv.Logins(), 2)
	require.NoError(t, c.Send(line))
	assert.Equal(t, []string{line}, lines(t, srv, 1))
}

func TestAClientWithoutAServerSendsNothing(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	addr := srv.Addr()
	srv.Close()
	c := aprs.NewClient(aprs.Config{Login: "K0CALL-10", Passcode: "12345", Server: addr}, "1")
	c.Start(context.Background())
	defer c.Stop()
	assert.ErrorIs(t, c.Send(line), aprs.ErrNotLoggedIn)
}

func TestTheClientLogsInWithItsFilterAndReadsWhatTheServerSends(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c := aprs.NewClient(aprs.Config{Login: "K0CALL-10", Passcode: "12345", Server: srv.Addr(), Filter: "g/N1CALL/N1CALL-*"}, "1")
	logins := make(chan bool, 1)
	c.OnLogin(func(verified bool) { logins <- verified })
	got := make(chan string, 4)
	c.OnLine(func(line string) { got <- line })
	c.Start(context.Background())
	t.Cleanup(c.Stop)
	require.True(t, reply(t, logins))
	assert.Equal(t, []string{"user K0CALL-10 pass 12345 vers DMRHub 1 filter g/N1CALL/N1CALL-*"}, srv.Logins())
	msg := "N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 :Hello{1"
	srv.Push(msg)
	ctx, cancel := context.WithTimeout(t.Context(), aprs.ReadTimeout)
	defer cancel()
	select {
	case line := <-got:
		assert.Equal(t, msg, line)
	case <-ctx.Done():
		t.Fatal("the client did not read the line")
	}
	assert.Empty(t, got, "the client handed on a server comment")
}
