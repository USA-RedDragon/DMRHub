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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResumeTokens(t *testing.T) {
	t.Parallel()
	s, _ := webTransportServer(t)
	now := time.Now()
	token := s.resumeToken(311860, now)

	id, ok := s.checkResumeToken(token, now)
	assert.True(t, ok, "a fresh token is good")
	assert.Equal(t, uint32(311860), id)

	_, ok = s.checkResumeToken(token, now.Add(2*resumeTTL))
	assert.False(t, ok, "an old token is not good")

	b := []byte(token)
	b[len(b)-1] ^= 1
	if b[len(b)-1] == token[len(token)-1] {
		b[len(b)-1]++
	}
	_, ok = s.checkResumeToken(string(b), now)
	assert.False(t, ok, "a token with another MAC is not good")
	_, ok = s.checkResumeToken("not a token", now)
	assert.False(t, ok)

	other := &Server{clock: clock.Real(), resumeKey: resumeKey([]byte("another secret"))}
	_, ok = other.checkResumeToken(token, now)
	assert.False(t, ok, "a hub with another secret does not take the token")

	ctx := context.Background()
	assert.True(t, s.spendToken(ctx, token), "a token is spent once")
	assert.False(t, s.spendToken(ctx, token), "a spent token moves no session again")
}

func wtLogin(t *testing.T, s *Server, addr wtAddr, id uint32) {
	t.Helper()
	ep := endpoint{transport: webTransportName, addr: addr}
	handle := func(p []byte) {
		s.handleFrom(context.Background(), ep, p, time.Now())
		s.lookups.Wait()
		s.records.Wait()
	}
	rptl, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	handle(rptl)
	tok, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte("s3cret"))
	require.NoError(t, err)
	rptk, err := (&dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: tok}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	handle(rptk)
	handle(rptc(t, dmrd.DialectMMDVM, id))
	_, ok := s.connected.Load(uint(id))
	require.True(t, ok, "the hotspot %d should be logged in", id)
}

// TestResumeTokenSurvivesABusySession asks a session that already carries
// a login to resume another one, then asks a fresh session with the same
// token. The refusal of the busy session moved nothing, so it must not
// spend the token; the token still moves the session only once.
func TestResumeTokenSurvivesABusySession(t *testing.T) {
	t.Parallel()
	s, _ := webTransportServer(t)
	s.mu.Lock()
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	s.mu.Unlock()
	wtLogin(t, s, wtAddr("old"), 311860)
	wtLogin(t, s, wtAddr("busy"), 311861)

	token := s.resumeToken(311860, time.Now())
	r := s.resumeWebTransport(wtAddr("busy"), token)
	assert.False(t, r.Resumed)
	assert.Equal(t, "busy", r.Reason)

	r = s.resumeWebTransport(wtAddr("new"), token)
	assert.True(t, r.Resumed, "a refusal that moved nothing must leave the token good, got %q", r.Reason)

	r = s.resumeWebTransport(wtAddr("third"), token)
	assert.False(t, r.Resumed)
	assert.Equal(t, "the token was used", r.Reason)
}
