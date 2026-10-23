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

const line = "KI5VMF-9>APZDMH,W5ABC-R*,qAR,N0HUB-10:!3016.03N/09744.59W>"

func start(t *testing.T, srv *aprstest.Server, passcode string) *aprs.Client {
	t.Helper()
	c := aprs.NewClient(aprs.Config{Login: "N0HUB-10", Passcode: passcode, Server: srv.Addr()}, "1.2.3 dirty")
	aprs.SetBackoff(c, 50*time.Millisecond)
	c.Start(context.Background())
	t.Cleanup(c.Stop)
	return c
}

func TestTheClientLogsInAndSends(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c := start(t, srv, "12345")
	require.Eventually(t, c.LoggedIn, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"user N0HUB-10 pass 12345 vers DMRHub 1.2.3-dirty"}, srv.Logins())
	require.NoError(t, c.Send(line))
	assert.Equal(t, []string{line}, srv.WaitLines(1, 3*time.Second))
}

func TestAnUnverifiedClientSendsNothing(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c := start(t, srv, "-1")
	require.Eventually(t, func() bool { return len(srv.Logins()) == 1 }, 3*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	assert.False(t, c.LoggedIn())
	assert.ErrorIs(t, c.Send(line), aprs.ErrNotLoggedIn)
}

func TestTheClientLogsInAgainAfterTheServerDrops(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	c := start(t, srv, "12345")
	require.Eventually(t, c.LoggedIn, 3*time.Second, 10*time.Millisecond)
	srv.Drop()
	require.Eventually(t, func() bool { return len(srv.Logins()) == 2 }, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, c.LoggedIn, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, c.Send(line))
	assert.Equal(t, []string{line}, srv.WaitLines(1, 3*time.Second))
}

func TestAClientWithoutAServerSendsNothing(t *testing.T) {
	t.Parallel()
	srv := aprstest.NewServer(t, "12345")
	addr := srv.Addr()
	srv.Close()
	c := aprs.NewClient(aprs.Config{Login: "N0HUB-10", Passcode: "12345", Server: addr}, "1")
	c.Start(context.Background())
	defer c.Stop()
	assert.ErrorIs(t, c.Send(line), aprs.ErrNotLoggedIn)
}
