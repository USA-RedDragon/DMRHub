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
package migration_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddPeerRuleMatchKeepsRules makes peer rules as they were before rules
// had a match. After the migration an ingress rule checks the destination
// and an egress rule the source, as they did before.
func TestAddPeerRuleMatchKeepsRules(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			memdb.Use(&cfg)
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			require.NoError(t, database.Create(&models.User{ID: 3000001, Callsign: "N0CALL", Username: "n0call", Password: "hash"}).Error)
			require.NoError(t, database.Create(&models.Peer{ID: 500001, OwnerID: 3000001, Password: "s3cret", Ingress: true, Egress: true}).Error)

			m := database.Migrator()
			require.NoError(t, m.DropColumn(&models.PeerRule{}, "Match"))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610271400").Error)
			insert := "INSERT INTO peer_rules (peer_id, direction, subject_id_min, subject_id_max) VALUES (?, ?, ?, ?)"
			require.NoError(t, database.Exec(insert, 500001, true, 3100, 3100).Error)
			require.NoError(t, database.Exec(insert, 500001, false, 3000001, 3000099).Error)

			require.NoError(t, migration.Migrate(database, &cfg))

			require.True(t, m.HasColumn(&models.PeerRule{}, "Match"))
			ingress, err := models.ListIngressRulesForPeer(database, 500001)
			require.NoError(t, err)
			require.Len(t, ingress, 1)
			assert.Equal(t, models.PeerRuleMatchDestination, ingress[0].Match)
			egress, err := models.ListEgressRulesForPeer(database, 500001)
			require.NoError(t, err)
			require.Len(t, egress, 1)
			assert.Equal(t, models.PeerRuleMatchSource, egress[0].Match)

			assert.True(t, rules.IngressAllows(ingress, &models.Packet{Src: 1, Dst: 3100, GroupCall: true}))
			assert.False(t, rules.IngressAllows(ingress, &models.Packet{Src: 3100, Dst: 91, GroupCall: true}))
			assert.True(t, rules.EgressAllows(egress, &models.Packet{Src: 3000050, Dst: 91, GroupCall: true}))
			assert.False(t, rules.EgressAllows(egress, &models.Packet{Src: 3100001, Dst: 3000050, GroupCall: true}))
		})
	}
}
