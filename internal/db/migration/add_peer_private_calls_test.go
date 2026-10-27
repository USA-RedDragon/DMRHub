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

// TestAddPeerPrivateCallsKeepsRules makes peers as they were before the
// private call switches. After the migration both switches are off and the
// rules let through the calls they did before: a private call needs a
// destination rule that covers its target, or no destination rules.
func TestAddPeerPrivateCallsKeepsRules(t *testing.T) {
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
			m := database.Migrator()
			require.NoError(t, m.DropColumn(&models.Peer{}, "IngressPrivate"))
			require.NoError(t, m.DropColumn(&models.Peer{}, "EgressPrivate"))
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610271410").Error)
			insertPeer := "INSERT INTO peers (id, owner_id, password, ingress, egress) VALUES (?, ?, ?, ?, ?)"
			require.NoError(t, database.Exec(insertPeer, 500001, 3000001, "s3cret", true, true).Error)
			require.NoError(t, database.Exec(insertPeer, 500002, 3000001, "s3cret", true, true).Error)
			for _, r := range []models.PeerRule{
				{PeerID: 500001, Direction: true, Match: models.PeerRuleMatchDestination, SubjectIDMin: 3100, SubjectIDMax: 3100},
				{PeerID: 500001, Direction: false, Match: models.PeerRuleMatchDestination, SubjectIDMin: 3100, SubjectIDMax: 3100},
				{PeerID: 500002, Direction: true, Match: models.PeerRuleMatchSource, SubjectIDMin: 3000001, SubjectIDMax: 3000099},
				{PeerID: 500002, Direction: false, Match: models.PeerRuleMatchSource, SubjectIDMin: 3000001, SubjectIDMax: 3000099},
			} {
				require.NoError(t, database.Create(&r).Error)
			}

			require.NoError(t, migration.Migrate(database, &cfg))

			require.True(t, m.HasColumn(&models.Peer{}, "IngressPrivate"))
			require.True(t, m.HasColumn(&models.Peer{}, "EgressPrivate"))
			private := &models.Packet{Src: 3000050, Dst: 3100001}
			for _, c := range []struct {
				id   uint
				want bool
			}{{500001, false}, {500002, true}} {
				peer, err := models.FindPeerByID(database, c.id)
				require.NoError(t, err)
				assert.False(t, peer.IngressPrivate)
				assert.False(t, peer.EgressPrivate)
				ingress, err := models.ListIngressRulesForPeer(database, c.id)
				require.NoError(t, err)
				egress, err := models.ListEgressRulesForPeer(database, c.id)
				require.NoError(t, err)
				assert.Equal(t, c.want, rules.IngressAllows(ingress, peer.IngressPrivate, private), "peer %d ingress", c.id)
				assert.Equal(t, c.want, rules.EgressAllows(egress, peer.EgressPrivate, private), "peer %d egress", c.id)
			}
		})
	}
}
