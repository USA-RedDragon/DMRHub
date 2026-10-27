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
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDMRAAndDMRGAreRecorded(t *testing.T) {
	t.Parallel()
	const repeaterID, radio = uint(311862), uint(3191868)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_alias_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})
	s := Server{DB: database, hub: h}

	lcs, err := pdu.EncodeTalkerAlias("KI5VMF", layer3.TalkerAliasSevenBit)
	require.NoError(t, err)
	for i := range lcs {
		a := dmrd.TalkerAlias{Repeater: uint32(repeaterID), Source: uint32(radio), Type: uint8(i), Data: lcs[i].RawPayload, Size: 19}
		p, err := a.AppendBinary(nil)
		require.NoError(t, err)
		s.data(context.Background(), dmrdsession.Output{Kind: dmrdsession.OutputData, Command: dmrd.CommandDMRA, ID: uint32(repeaterID), Packet: p}, time.Now())
	}
	pos := dmrd.Position{Repeater: uint32(repeaterID), Source: uint32(radio), Size: 14}
	p, err := pos.AppendBinary(nil)
	require.NoError(t, err)
	s.data(context.Background(), dmrdsession.Output{Kind: dmrdsession.OutputData, Command: dmrd.CommandDMRG, ID: uint32(repeaterID), Packet: p}, time.Now())

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = nil
		return database.Where("active = ?", false).Order("kind").Find(&calls).Error == nil && len(calls) == 2
	}, 5*time.Second, 50*time.Millisecond)
	assert.Equal(t, models.CallKindAlias, calls[0].Kind)
	assert.Equal(t, "KI5VMF", calls[0].TalkerAlias)
	assert.Equal(t, models.CallKindGPS, calls[1].Kind)
	assert.True(t, calls[1].Partial)
	for _, c := range calls {
		assert.Equal(t, radio, c.SourceID)
		assert.Equal(t, repeaterID, c.IngressID)
		assert.Equal(t, models.RepeaterTypeMMDVM, c.Transport)
	}
}
