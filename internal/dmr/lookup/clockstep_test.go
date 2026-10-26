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

package lookup_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/clockstep"
	"github.com/stretchr/testify/require"
)

func TestClockStepForwardKeepsAnAnswerReadJustNow(t *testing.T) { //nolint:paralleltest
	var loads atomic.Int64
	c := lookup.New("test", time.Hour, func(int) (int, error) {
		loads.Add(1)
		return 1, nil
	})
	_, err := c.Get(1)
	require.NoError(t, err)
	clockstep.Step(t, 2*lookup.Idle)
	c.Prune(time.Now())
	_, err = c.Get(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), loads.Load(), "a step of the wall clock pruned an answer read just now")
}

func TestClockStepBackStillPrunesAnIdleAnswer(t *testing.T) { //nolint:paralleltest
	var loads atomic.Int64
	c := lookup.New("test", time.Hour, func(int) (int, error) {
		loads.Add(1)
		return 1, nil
	})
	_, err := c.Get(1)
	require.NoError(t, err)
	clockstep.Step(t, -2*lookup.Idle)
	c.Prune(time.Now().Add(lookup.Idle + time.Second))
	_, err = c.Get(1)
	require.NoError(t, err)
	require.Equal(t, int64(2), loads.Load(), "a step back of the wall clock kept an answer nobody read for Idle")
}
