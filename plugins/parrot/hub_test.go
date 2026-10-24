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

package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

const (
	caller   = uint(1000002)
	repeater = uint(100003)
	parrotID = uint(9990)
)

// TestTheParrotAnswersThroughTheHub sends the captured call from a radio on
// a repeater to the parrot's ID and checks that the parrot, connected over
// TLS, plays it back to that repeater.
func TestTheParrotAnswersThroughTheHub(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Secret = "a secret of the config, long enough"
	cfg.Database.Database = fmt.Sprintf("file:memdb_parrotplugin_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, calltracker.NewCallTracker(database, ps, kvStore))
	t.Cleanup(func() {
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})

	require.NoError(t, database.Create(&models.User{ID: caller, Callsign: "CALLER", Username: "CALLER", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{Callsign: "T1ST", ID: repeater},
		OwnerID:               caller,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), repeater)

	_, token, err := models.CreatePlugin(database, "Parrot", []models.PluginRange{{First: parrotID, Last: parrotID}}, time.Now())
	require.NoError(t, err)
	listener, err := plugins.Start(context.Background(), &cfg, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) })
	require.NoError(t, err)
	t.Cleanup(listener.Stop)
	certs, err := plugins.NewCertSource(cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	fp, err := certs.Fingerprint()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-done
	})
	go func() {
		defer close(done)
		run(ctx, plugin.Options{Address: listener.Addr().String(), Token: token, Fingerprint: fp})
	}()
	require.Eventually(t, func() bool {
		p, err := models.FindPlugin(database, 1)
		return err == nil && p.Online(time.Now())
	}, 5*time.Second, 20*time.Millisecond, "the parrot connects")

	call := hubCall(t)
	for _, p := range call {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		time.Sleep(burstTime)
	}
	for i := range call {
		select {
		case rp := <-srv.Packets:
			require.Equal(t, repeater, rp.RepeaterID, "burst %d", i)
			got := rp.Packet
			require.Equal(t, parrotID, got.Src, "burst %d", i)
			require.Equal(t, caller, got.Dst, "burst %d", i)
			require.False(t, got.GroupCall, "burst %d", i)
		case <-time.After(replyDelay + 3*time.Second):
			t.Fatalf("burst %d of the reply did not reach the repeater", i)
		}
	}
}

// hubCall returns the call of DMRGateway in
// ../../internal/dmr/testdata/dmrgateway-master.txt as a private call from
// the caller on the repeater to the parrot.
func hubCall(t *testing.T) []models.Packet {
	t.Helper()
	f, err := os.Open("../../internal/dmr/testdata/dmrgateway-master.txt")
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
		p.Src, p.Dst, p.Repeater, p.GroupCall = caller, parrotID, repeater, false
		out = append(out, p)
	}
	return out
}
