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

package modem_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
)

// TestServerStatusReachesTheOtherReplicas shows the live state of the
// modem in the server and in the KV store, where the other replicas read
// it, and that a stop removes it.
func TestServerStatusReachesTheOtherReplicas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := newFakes(t, 2)
	cfg := modemConfig(t, false)
	s := modem.NewServerWithOptions(cfg, nil, nil, "test", modem.Options{Open: f.open, Reply: 200 * time.Millisecond,
		Reopen: time.Hour, Retry: time.Hour, KV: store, Replica: "r1"})
	if st := s.Status(); st.Linked || st.Port != "/dev/fake-modem" || st.Baud != 115200 {
		t.Fatalf("before start: %+v", st)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m := f.next(t)
	if !m.WaitFor(waitLink, polled(1)) {
		t.Fatal("no link")
	}
	var shared []modem.Status
	deadline := time.Now().Add(waitLink)
	for time.Now().Before(deadline) {
		shared = modem.Statuses(ctx, store)
		if len(shared) == 1 && shared[0].Linked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := s.Status()
	if !st.Linked || st.Replica != "r1" || st.Firmware != "MMDVM fake 20261026" || st.Protocol != 2 ||
		!slices.Equal(st.Modes, []string{"DMR", "D-Star"}) || st.Since.IsZero() {
		t.Fatalf("linked: %+v", st)
	}
	if len(shared) != 1 || !shared[0].Linked || shared[0].Replica != "r1" || shared[0].Firmware != st.Firmware {
		t.Fatalf("in the store: %+v", shared)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := modem.Statuses(ctx, store); len(got) != 0 {
		t.Fatalf("after stop: %+v", got)
	}
}
