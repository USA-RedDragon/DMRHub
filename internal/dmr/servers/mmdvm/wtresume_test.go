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

	"github.com/stretchr/testify/assert"
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

	other := &Server{resumeKey: resumeKey([]byte("another secret"))}
	_, ok = other.checkResumeToken(token, now)
	assert.False(t, ok, "a hub with another secret does not take the token")

	ctx := context.Background()
	assert.True(t, s.spendToken(ctx, token), "a token is spent once")
	assert.False(t, s.spendToken(ctx, token), "a spent token moves no session again")
}
