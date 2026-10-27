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

package modem_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/stretchr/testify/require"
)

func TestServerCreatesAMissingRepeaterWithNoOwner(t *testing.T) {
	t.Parallel()
	cfg := modemConfig(t, false)
	cfg.DMR.Modem.CWID.Callsign = "ki5vmf"
	memdb.Use(cfg, "_pragma=foreign_keys(1)")
	database, err := db.MakeDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	f := newFakes(t, 1)
	s := modem.NewServerWithOptions(cfg, nil, database, "test", modem.Options{Open: f.open, Reply: 200 * time.Millisecond,
		Reopen: 50 * time.Millisecond, Retry: 50 * time.Millisecond})
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	m := f.next(t)
	require.True(t, m.WaitFor(waitLink, polled(2)), "no status requests")
	require.True(t, s.Linked(), "the server did not link a modem whose repeater did not exist")

	r, err := models.FindRepeaterByID(database, testID)
	require.NoError(t, err)
	require.Equal(t, models.RepeaterTypeModem, r.Type)
	require.Zero(t, r.OwnerID)
	require.Equal(t, "KI5VMF", r.Callsign)
	require.EqualValues(t, 438800000, r.RXFrequency)
	require.EqualValues(t, 438800000, r.TXFrequency)
	require.EqualValues(t, 1, r.ColorCode)
	require.EqualValues(t, 4, r.Slots)
	n, err := models.CountUnassignedRepeaters(database)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
