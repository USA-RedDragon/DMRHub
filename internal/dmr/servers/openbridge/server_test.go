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

package openbridge_test

import (
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
)

func TestMakeServer(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	assert.NoError(t, err)
	defer func() { _ = kvStore.Close() }()

	defConfig.DMR.OpenBridge.Port = 0

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, kvStore)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.NotNil(t, server.Buffer)
	assert.Len(t, server.Buffer, 73) // largestMessageSize
	assert.NotNil(t, server.DB)
}

func TestMakeServerDefaultBindAddress(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	defConfig.DMR.OpenBridge.Bind = "0.0.0.0"
	defConfig.DMR.OpenBridge.Port = 62035

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	assert.NoError(t, err)
	defer func() { _ = kvStore.Close() }()

	server, err := openbridge.MakeServer(&defConfig, nil, database, ps, kvStore)
	assert.NoError(t, err)
	defer func() { _ = server.Server.Close() }()

	assert.Equal(t, 62035, server.SocketAddress.Port)
	assert.Equal(t, "0.0.0.0", server.SocketAddress.IP.String())
}
