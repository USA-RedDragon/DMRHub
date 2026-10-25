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

package inside_test

import (
	"bufio"
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/inside"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

const (
	caller   = uint(1000002)
	repeater = uint(100003)
)

// call returns the call of DMRGateway in
// ../../dmr/testdata/dmrgateway-master.txt as a private call from the
// caller on the repeater to the parrot.
func call(t *testing.T) []models.Packet {
	t.Helper()
	f, err := os.Open("../../dmr/testdata/dmrgateway-master.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out []models.Packet
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 4 || fs[1] != "r>m" || !strings.HasPrefix(fs[3], "444d5244") {
			continue
		}
		raw, err := hex.DecodeString(fs[3])
		require.NoError(t, err)
		p, ok := models.UnpackPacket(raw)
		require.True(t, ok)
		p.Src, p.Dst, p.Repeater, p.GroupCall = caller, 9990, repeater, false
		out = append(out, p)
	}
	return out
}

// TestTheBuiltinParrotAnswersThroughTheHub sends the captured call from a
// radio on a repeater to 9990, and checks that the parrot that runs inside
// DMRHub plays it back to that repeater.
func TestTheBuiltinParrotAnswersThroughTheHub(t *testing.T) {
	t.Parallel()
	h, database, ps, srv := testutils.RepeaterHub(t, caller, repeater)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	listener, err := plugins.Start(context.Background(), &cfg, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	require.NoError(t, err)
	t.Cleanup(listener.Stop)
	m := inside.NewManager(inside.Deps{
		DB: database, PubSub: ps, KV: store, Instance: "a", Version: "v1.3.4",
		Service: listener.Service(), Registry: listener.Registry(), Engine: engine, Timing: fast(),
	})
	require.NoError(t, m.Start(context.Background(), true))
	t.Cleanup(func() { m.Stop(context.Background()) })
	parrot := builtinOf(t, &cluster{db: database}, "parrot")
	require.Eventually(t, func() bool {
		p, err := models.FindPlugin(database, parrot.ID)
		owner, owned := listener.Registry().Owner(9990)
		return err == nil && p.Online(time.Now()) && owned && owner == parrot.ID
	}, 60*time.Second, 50*time.Millisecond, "the parrot runs")

	sent := call(t)
	for _, p := range sent {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		time.Sleep(60 * time.Millisecond)
	}
	for i := range sent {
		select {
		case rp := <-srv.Packets:
			require.Equal(t, repeater, rp.RepeaterID, "burst %d", i)
			require.Equal(t, uint(9990), rp.Packet.Src, "burst %d", i)
			require.Equal(t, caller, rp.Packet.Dst, "burst %d", i)
			require.False(t, rp.Packet.GroupCall, "burst %d", i)
		case <-time.After(30 * time.Second):
			t.Fatalf("burst %d of the reply did not reach the repeater", i)
		}
	}
}
