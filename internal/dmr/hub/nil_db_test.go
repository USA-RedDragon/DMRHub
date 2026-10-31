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
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

func TestAHubWithNoDatabaseOutlivesItsResync(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := hub.NewHub(nil, nil, nil, nil)
		time.Sleep(3*hub.RepeaterCacheTTL + time.Second)
		synctest.Wait()
		h.Stop(context.Background())
	})
}

func TestAHubWithNoDatabaseTakesEveryChangeNotice(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		ps, err := pubsub.MakePubSub(context.Background(), &cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ps.Close() })
		one, two := hub.NewHub(nil, nil, ps, nil), hub.NewHub(nil, nil, ps, nil)
		t.Cleanup(func() {
			one.Stop(context.Background())
			two.Stop(context.Background())
		})
		ctx := context.Background()
		two.TalkgroupChanged(9)
		two.UserChanged(1000001)
		two.PeerChanged(3100100)
		two.RulesChanged(100001)
		two.RepeaterSecretChanged(100001)
		two.CodecChanged(100001)
		two.ReloadRepeater(ctx, 100001)
		synctest.Wait()
		time.Sleep(3*hub.RepeaterCacheTTL + time.Second)
		synctest.Wait()
	})
}
