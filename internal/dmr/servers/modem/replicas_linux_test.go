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

//go:build linux

package modem_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// cluster is two replicas that share a KV store and a pubsub, as replicas
// share Redis.
type cluster struct {
	store kv.KV
	ps    pubsub.PubSub
}

func newCluster(t *testing.T) cluster {
	t.Helper()
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := pubsub.MakePubSub(ctx, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.Close()
		_ = store.Close()
	})
	return cluster{store: store, ps: ps}
}

func (c cluster) replica(t *testing.T, id string, ports ...modem.SerialPort) *modem.Replicas {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reg := servers.NewInstanceRegistry(ctx, c.store, id)
	r := &modem.Replicas{ID: id, PubSub: c.ps, KV: c.store, Probe: probeOptions(),
		Ports: func() []modem.SerialPort { return ports }, PortsWait: time.Second, TestWait: 3 * time.Second}
	r.Start(ctx)
	t.Cleanup(func() {
		r.Stop()
		reg.Deregister(context.Background())
		cancel()
	})
	return r
}

func TestReplicasListThePortsOfEachReplica(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	a := c.replica(t, "a1", modem.SerialPort{Path: "/dev/ttyAMA0"})
	c.replica(t, "b2", modem.SerialPort{Path: "/dev/serial/by-id/usb-MMDVM-if00", Device: "/dev/ttyACM0"})
	start := time.Now()
	got := a.AllPorts(context.Background())
	if len(got) != 2 || got[0].Replica != "a1" || !got[0].Self || got[0].Ports[0].Path != "/dev/ttyAMA0" ||
		got[1].Replica != "b2" || got[1].Self || got[1].Ports[0].Device != "/dev/ttyACM0" {
		t.Fatalf("%+v", got)
	}
	if d := time.Since(start); d >= time.Second {
		t.Fatalf("waited %v though every replica answered", d)
	}
}

func TestReplicasTestOnTheReplicaOfThePort(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	fake := modemtest.New(master, 2)
	t.Cleanup(fake.Close)
	c := newCluster(t)
	a := c.replica(t, "a1", modem.SerialPort{Path: "/dev/ttyAMA0"})
	c.replica(t, "b2", modem.SerialPort{Path: slave})
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port = slave
	ans, err := a.Test(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if ans.Replica != "b2" || ans.Self || ans.Unanswered || ans.Live || ans.Result == nil || !ans.Result.Answered || !ans.Result.Settings.Taken {
		t.Fatalf("%+v %+v", ans, ans.Result)
	}
	ans, err = a.Test(context.Background(), cfg, "b2")
	if err != nil || ans.Replica != "b2" || ans.Result == nil || !ans.Result.Answered {
		t.Fatalf("with the replica given: %+v %v", ans, err)
	}
}

func TestReplicasWhenTheOtherReplicaIsGone(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	a := c.replica(t, "a1", modem.SerialPort{Path: "/dev/ttyAMA0"})
	b := c.replica(t, "b2", modem.SerialPort{Path: "/dev/ttyACM0"})
	b.Stop()
	got := a.AllPorts(context.Background())
	if len(got) != 1 || got[0].Replica != "a1" {
		t.Fatalf("ports: %+v", got)
	}
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port = "/dev/ttyACM0"
	start := time.Now()
	ans, err := a.Test(context.Background(), cfg, "b2")
	if err != nil || !ans.Unanswered || ans.Replica != "b2" || ans.Result != nil {
		t.Fatalf("%+v %v", ans, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("the test took %v", d)
	}
}

// TestReplicasShowTheModemOfAnotherReplica shows the live state of the
// modem of another replica instead of a test of its port.
func TestReplicasShowTheModemOfAnotherReplica(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	a := c.replica(t, "a1")
	c.replica(t, "b2", modem.SerialPort{Path: "/dev/fake-modem"})
	f := newFakes(t, 2)
	srv := modem.NewServerWithOptions(modemConfig(t, false), nil, nil, "test", modem.Options{Open: f.open, Reply: 200 * time.Millisecond,
		Reopen: time.Hour, Retry: time.Hour, KV: c.store, Replica: "b2"})
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	deadline := time.Now().Add(waitLink)
	for time.Now().Before(deadline) && (a.State(context.Background()).Status == nil || !a.State(context.Background()).Status.Linked) {
		time.Sleep(10 * time.Millisecond)
	}
	st := a.State(context.Background())
	if st.Status == nil || !st.Status.Linked || st.Self || st.Status.Replica != "b2" {
		t.Fatalf("state: %+v", st)
	}
	ans, err := a.Test(context.Background(), modemConfig(t, false).DMR.Modem, "")
	if err != nil || !ans.Live || ans.Replica != "b2" || ans.Self || ans.State.Status.Firmware != "MMDVM fake 20261026" {
		t.Fatalf("%+v %v", ans, err)
	}
}
