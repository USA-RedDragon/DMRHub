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

package connlog_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	testutils.Main(m)
}

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	memdb.CloseAtCleanup(t, database)
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100000101}, Password: "pw"}).Error)
	return database
}

// TestALogFoldsRepeatsAndKeepsOrder notes a burst of the same event, then
// another one, then the first again, and an event of a repeater that does
// not exist. The log holds three entries in order, the first with the
// count and the times of the burst, and nothing for the unknown repeater.
func TestALogFoldsRepeatsAndKeepsOrder(t *testing.T) {
	t.Parallel()
	database := newDB(t)
	l := connlog.New(database)
	t0 := time.Date(2026, 11, 1, 8, 12, 0, 0, time.UTC)
	for i := range 14 {
		l.Note(100000101, connlog.WrongPassword, "203.0.113.7", "", t0.Add(time.Duration(i)*time.Minute))
	}
	l.Wait()
	l.Note(100000101, connlog.WrongPassword, "203.0.113.7", "", t0.Add(20*time.Minute))
	l.Note(100000101, connlog.Connected, "203.0.113.7", "Pi-Star", t0.Add(30*time.Minute))
	l.Note(100000101, connlog.WrongPassword, "203.0.113.7", "", t0.Add(40*time.Minute))
	l.Note(100000199, connlog.Timeout, "", "", t0)
	l.Close()
	l.Note(100000101, connlog.Timeout, "", "", t0.Add(time.Hour))
	l.Wait()

	events, err := models.ListRepeaterEvents(database, 100000101, time.Time{})
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, connlog.WrongPassword, events[0].Kind)
	require.Equal(t, uint(1), events[0].Count)
	require.Equal(t, connlog.Connected, events[1].Kind)
	require.Equal(t, "Pi-Star", events[1].Detail)
	require.Equal(t, uint(15), events[2].Count)
	require.True(t, events[2].FirstAt.Equal(t0))
	require.True(t, events[2].LastAt.Equal(t0.Add(20*time.Minute)))
	var n int64
	require.NoError(t, database.Model(&models.RepeaterEvent{}).Where("repeater_id = ?", 100000199).Count(&n).Error)
	require.Zero(t, n)
}

// TestANilLogDropsEvents notes events on a nil Log and a Log without a
// database.
func TestANilLogDropsEvents(t *testing.T) {
	t.Parallel()
	var l *connlog.Log
	l.Note(1, connlog.Timeout, "", "", time.Now())
	l.Wait()
	l.Close()
	n := connlog.New(nil)
	n.Note(1, connlog.Timeout, "", "", time.Now())
	n.Close()
}
