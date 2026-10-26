//go:build clockstep

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

package transcode

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/clockstep"
	"github.com/stretchr/testify/require"
)

func TestClockStepForwardKeepsAServerThatJustAnswered(t *testing.T) { //nolint:paralleltest
	s := NewService(nil)
	srv := &server{leases: newLeases(nil, ServerConfig{})}
	list := []*server{srv}
	s.servers.Store(&list)
	srv.answer()
	clockstep.Step(t, time.Hour)
	st := s.Status(t.Context())
	require.Len(t, st, 1)
	require.True(t, st[0].Answered)
	require.Less(t, time.Since(st[0].AnsweredAt), time.Minute, "a step of the wall clock aged the last answer of the server")
}
