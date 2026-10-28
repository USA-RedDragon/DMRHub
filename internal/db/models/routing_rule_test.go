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

package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func backendDB(t *testing.T, be testutils.Backend) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	be.Migrated().Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

func forAllBackends(t *testing.T, f func(t *testing.T, database *gorm.DB)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, backendDB(t, be))
		})
	}
}

func TestRoutingRulesReplaceOnlyTheirKinds(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		const rpt, other = 311860, 311861
		for _, id := range []uint{rpt, other} {
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeMMDVM}).Error)
		}
		slots := []models.RoutingRuleKind{models.RoutingRuleSlotUser}
		tgs := []models.RoutingRuleKind{models.RoutingRuleBlockTalkgroup, models.RoutingRuleAllowTalkgroup}

		require.NoError(t, models.ReplaceRoutingRules(database, rpt, slots, []models.RoutingRule{
			{Kind: models.RoutingRuleSlotUser, Slot: 2, SubjectID: 3191868},
			{Kind: models.RoutingRuleSlotUser, Slot: 2, SubjectID: 3191868},
			{Kind: models.RoutingRuleSlotUser, Slot: 1, SubjectID: 3191869},
		}))
		require.NoError(t, models.ReplaceRoutingRules(database, rpt, tgs, []models.RoutingRule{{Kind: models.RoutingRuleBlockTalkgroup, SubjectID: 91}}))
		require.NoError(t, models.ReplaceRoutingRules(database, other, tgs, []models.RoutingRule{{Kind: models.RoutingRuleAllowTalkgroup, SubjectID: 3100}}))

		type row struct {
			kind models.RoutingRuleKind
			slot uint8
			id   uint
		}
		rows := func(id uint) []row {
			t.Helper()
			rules, err := models.ListRoutingRules(database, id)
			require.NoError(t, err)
			out := make([]row, 0, len(rules))
			for _, r := range rules {
				assert.Equal(t, id, r.RepeaterID)
				out = append(out, row{r.Kind, r.Slot, r.SubjectID})
			}
			return out
		}
		assert.ElementsMatch(t, []row{
			{models.RoutingRuleSlotUser, 2, 3191868},
			{models.RoutingRuleSlotUser, 1, 3191869},
			{models.RoutingRuleBlockTalkgroup, 0, 91},
		}, rows(rpt), "a repeated rule is kept once")

		require.NoError(t, models.ReplaceRoutingRules(database, rpt, tgs, []models.RoutingRule{{Kind: models.RoutingRuleAllowTalkgroup, SubjectID: 3100}}))
		assert.ElementsMatch(t, []row{
			{models.RoutingRuleSlotUser, 2, 3191868},
			{models.RoutingRuleSlotUser, 1, 3191869},
			{models.RoutingRuleAllowTalkgroup, 0, 3100},
		}, rows(rpt), "a talkgroup edit keeps the slot locks")

		require.NoError(t, models.ReplaceRoutingRules(database, rpt, slots, nil))
		assert.ElementsMatch(t, []row{{models.RoutingRuleAllowTalkgroup, 0, 3100}}, rows(rpt))

		require.Error(t, models.ReplaceRoutingRules(database, rpt, slots, []models.RoutingRule{{Kind: models.RoutingRuleBlockTalkgroup, SubjectID: 1}}),
			"a rule of a kind that the call does not replace")
		assert.ElementsMatch(t, []row{{models.RoutingRuleAllowTalkgroup, 0, 3100}}, rows(rpt))

		require.NoError(t, models.DeleteRepeater(database, rpt))
		assert.Empty(t, rows(rpt), "deleting a repeater deletes its rules")
		assert.ElementsMatch(t, []row{{models.RoutingRuleAllowTalkgroup, 0, 3100}}, rows(other))
	})
}

func TestDeletingAUserDeletesTheirRoutingRules(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, database *gorm.DB) {
		const owner, listed, kept = 3191868, 3191869, 3191870
		for _, id := range []uint{owner, listed, kept} {
			require.NoError(t, database.Create(&models.User{ID: id, Callsign: "N0CAL" + string(rune('A'+id-owner)), Username: "u" + string(rune('a'+id-owner)), Approved: true}).Error)
		}
		const owned, other = 311860, 311861
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: owned}, OwnerID: owner, Type: models.RepeaterTypeMMDVM}).Error)
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: other}, OwnerID: kept, Type: models.RepeaterTypeMMDVM}).Error)
		slots := []models.RoutingRuleKind{models.RoutingRuleSlotUser}
		require.NoError(t, models.ReplaceRoutingRules(database, owned, slots, []models.RoutingRule{{Kind: models.RoutingRuleSlotUser, Slot: 1, SubjectID: kept}}))
		require.NoError(t, models.ReplaceRoutingRules(database, other, slots, []models.RoutingRule{
			{Kind: models.RoutingRuleSlotUser, Slot: 1, SubjectID: listed},
			{Kind: models.RoutingRuleSlotUser, Slot: 1, SubjectID: kept},
		}))

		require.NoError(t, models.DeleteUser(database, owner))
		rules, err := models.ListRoutingRules(database, owned)
		require.NoError(t, err)
		assert.Empty(t, rules, "the rules of a deleted user's repeater go with it")

		require.NoError(t, models.DeleteUser(database, listed))
		rules, err = models.ListRoutingRules(database, other)
		require.NoError(t, err)
		require.Len(t, rules, 1)
		assert.EqualValues(t, kept, rules[0].SubjectID)
	})
}
