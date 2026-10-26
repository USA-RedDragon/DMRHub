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

package hub_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/clockstep"
	"github.com/stretchr/testify/require"
)

func TestClockStepBackStillLogsAnUndeliverableBurst(t *testing.T) { //nolint:paralleltest
	var logs lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	h := hub.NewHub(nil, nil, nil, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })

	h.ReportUndeliverable(models.Packet{StreamID: 1}, "test", "test")
	clockstep.Step(t, -time.Hour)
	time.Sleep(1100 * time.Millisecond)
	h.ReportUndeliverable(models.Packet{StreamID: 2}, "test", "test")
	require.Equal(t, 2, strings.Count(logs.String(), "Burst cannot go to its target"), "a step back of the wall clock held the log of a new stream: %s", logs.String())
}
