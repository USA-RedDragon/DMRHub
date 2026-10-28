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
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/require"
)

func TestEphemeralTTLTakesEffectWhenTheConfigIsSet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		time.Sleep(300 * time.Millisecond)
		old := time.Now().Add(-time.Hour)
		require.NoError(t, database.Create(&models.Talkgroup{ID: 3999, Name: "TG 3999", AutoCreated: true, LastCallAt: &old}).Error)
		cfg := config.Config{}
		cfg.DMR.Routing = config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: time.Second}
		h.SetConfig(config.NewCurrent(&cfg))
		bubble.Eventually(t, func() bool {
			exists, err := models.TalkgroupIDExists(database, 3999)
			return err == nil && !exists
		}, 5*time.Second, 20*time.Millisecond, "a talkgroup past the TTL goes soon after the config turns ephemeral talkgroups on")
	})
}
