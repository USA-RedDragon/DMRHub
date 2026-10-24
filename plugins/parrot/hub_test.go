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

package parrot

import (
	"bufio"
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
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
	h, database, ps, srv := testutils.RepeaterHub(t, caller, repeater)
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	listener.Run(t, "Parrot", []models.PluginRange{{First: parrotID, Last: parrotID}}, Serve)

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
