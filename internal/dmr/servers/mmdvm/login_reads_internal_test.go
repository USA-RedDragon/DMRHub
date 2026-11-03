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
	"errors"
	"net"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type statements struct {
	logger.Interface
	mu  sync.Mutex
	sql []string
}

func (s *statements) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	q, _ := fc()
	s.mu.Lock()
	s.sql = append(s.sql, q)
	s.mu.Unlock()
}

func (s *statements) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.sql
	s.sql = nil
	return out
}

var repeaterRead = regexp.MustCompile("(?i)^SELECT .* FROM [`\"]?repeaters[`\"]? ")

func readsOfRepeaters(sql []string) int {
	n := 0
	for _, q := range sql {
		if repeaterRead.MatchString(q) {
			n++
		}
	}
	return n
}

func loginDatabase(t *testing.T) (*gorm.DB, *statements) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{Callsign: "T0001ST", ID: 100001},
		OwnerID:               1000001,
		Type:                  models.RepeaterTypeMMDVM,
		Password:              "s3cret",
	}).Error)
	rec := &statements{Interface: logger.Discard}
	return database.Session(&gorm.Session{Logger: rec}), rec
}

// TestTheLoginPasswordIsOneRead looks up the password of a login. It must
// take one statement, since a login waits on it and every statement of a
// SQLite database takes turns with the others.
func TestTheLoginPasswordIsOneRead(t *testing.T) {
	t.Parallel()
	database, rec := loginDatabase(t)
	s := &Server{DB: database}

	password, err := s.password(100001)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", password)
	assert.Len(t, rec.take(), 1, "the password lookup ran more than one statement")

	_, err = s.password(100009)
	require.Error(t, err)
	assert.Len(t, rec.take(), 1, "the lookup of an unknown repeater ran more than one statement")

	require.NoError(t, database.Exec("UPDATE repeaters SET password = ? WHERE id = ?", "", 100001).Error)
	rec.take()
	_, err = s.password(100001)
	assert.True(t, errors.Is(err, errNoPassword), "a repeater without a password must be refused, got %v", err)

	require.NoError(t, database.Delete(&models.Repeater{}, 100001).Error)
	rec.take()
	_, err = s.password(100001)
	require.Error(t, err, "a deleted repeater must be refused")
}

// TestTakingAConfigReadsTheRepeaterOnce takes the RPTC of a login. It must
// read the repeater once and save the config.
func TestTakingAConfigReadsTheRepeaterOnce(t *testing.T) {
	t.Parallel()
	database, rec := loginDatabase(t)
	s := &Server{DB: database, clock: clock.Real(), Version: "test", Commit: "test"}

	var cfg dmrd.MMDVMConfig
	cfg.Reset()
	cfg.SetID(100001)
	require.NoError(t, errors.Join(
		cfg.SetCallsign([]byte("T0001ST")),
		cfg.SetRXFrequency(440000000),
		cfg.SetTXFrequency(445000000),
		cfg.SetPower(10),
		cfg.SetColorCode(1),
		cfg.SetLatitude([]byte("+00.0000")),
		cfg.SetLongitude([]byte("+000.0000")),
		cfg.SetHeight(10),
		cfg.SetLocation([]byte("Test")),
		cfg.SetDescription([]byte("Login reads")),
		cfg.SetSlots(3),
		cfg.SetSoftwareID([]byte("20260713")),
		cfg.SetPackageID([]byte("MMDVM")),
	))
	ep := udpEndpoint(net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 62031})

	rec.take()
	got, ok := s.takeConfig(100001, ep, dmrd.DialectMMDVM, cfg[:])
	require.True(t, ok)
	assert.Equal(t, 1, readsOfRepeaters(rec.take()), "the config of a login read the repeater more than once")
	assert.Equal(t, uint(440000000), got.RXFrequency)

	saved, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	assert.Equal(t, uint(445000000), saved.TXFrequency)
	assert.Equal(t, "Login reads", saved.Description)
	assert.Equal(t, "s3cret", saved.Password)
}
