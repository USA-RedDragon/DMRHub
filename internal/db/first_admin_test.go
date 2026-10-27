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

package db_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const claimers = 12

func allBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

// TestClaimFirstAdminOnceAcrossReplicas has visitors of two replicas, each
// with its own connections to one database, make the first admin at once.
// The database lets exactly one through, on every database, and no one
// after.
func TestClaimFirstAdminOnceAcrossReplicas(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			var replicas [2]*gorm.DB
			for i := range replicas {
				gdb, err := db.MakeDB(&cfg)
				require.NoError(t, err)
				t.Cleanup(func() { closeDB(t, gdb) })
				replicas[i] = gdb
			}
			open, err := models.FirstAdminOpen(replicas[0])
			require.NoError(t, err)
			require.True(t, open)

			start := make(chan struct{})
			errs := make([]error, claimers)
			var wg sync.WaitGroup
			for i := range claimers {
				wg.Go(func() {
					<-start
					u := models.User{ID: uint(3000001 + i), Callsign: fmt.Sprintf("N%dCALL", i), Username: fmt.Sprintf("admin%d", i), Password: "hash"}
					errs[i] = models.ClaimFirstAdmin(replicas[i%len(replicas)], &u)
				})
			}
			close(start)
			wg.Wait()

			won := 0
			for i, err := range errs {
				if err == nil {
					won++
					continue
				}
				assert.ErrorIs(t, err, models.ErrFirstAdminClaimed, "visitor %d", i)
			}
			assert.Equal(t, 1, won, "visitors that made the first admin")
			var supers, claims int64
			require.NoError(t, replicas[1].Model(&models.User{}).Where("super_admin = ? AND admin = ? AND approved = ?", true, true, true).Count(&supers).Error)
			require.NoError(t, replicas[1].Model(&models.FirstAdmin{}).Count(&claims).Error)
			assert.Equal(t, int64(1), supers)
			assert.Equal(t, int64(1), claims)
			var users int64
			require.NoError(t, replicas[1].Model(&models.User{}).Count(&users).Error)
			assert.Equal(t, int64(1), users, "a visitor that lost made no account")

			open, err = models.FirstAdminOpen(replicas[1])
			require.NoError(t, err)
			assert.False(t, open)
			late := models.User{ID: 3000099, Callsign: "N9CALL", Username: "late", Password: "hash"}
			assert.ErrorIs(t, models.ClaimFirstAdmin(replicas[1], &late), models.ErrFirstAdminClaimed)
		})
	}
}

// TestClaimFirstAdminRefusesWhenASuperAdminExists makes the first admin on
// a database of before the first admin row, which has a super admin.
func TestClaimFirstAdminRefusesWhenASuperAdminExists(t *testing.T) {
	t.Parallel()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg := backendConfig(t, be)
			gdb, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() { closeDB(t, gdb) })
			require.NoError(t, gdb.Create(&models.User{ID: 3000001, Callsign: "N0CALL", Username: "owner", Password: "hash", Approved: true, Admin: true, SuperAdmin: true}).Error)

			open, err := models.FirstAdminOpen(gdb)
			require.NoError(t, err)
			assert.False(t, open)
			u := models.User{ID: 3000002, Callsign: "N1CALL", Username: "visitor", Password: "hash"}
			err = models.ClaimFirstAdmin(gdb, &u)
			assert.True(t, errors.Is(err, models.ErrFirstAdminClaimed), "got %v", err)
			var claims int64
			require.NoError(t, gdb.Model(&models.FirstAdmin{}).Count(&claims).Error)
			assert.Zero(t, claims, "a refused claim leaves no row")
		})
	}
}
