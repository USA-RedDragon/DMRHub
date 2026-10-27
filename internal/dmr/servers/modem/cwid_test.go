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

package modem

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

func cwBench(t *testing.T, duplex bool) *bench {
	t.Helper()
	return newBenchWith(t, duplex, 2, rssiMap{}, func(m *config.Modem) {
		m.CWID = config.ModemCWID{Enabled: true, Time: 1, Callsign: "ki5vmf"}
	})
}

// cwIDs returns the callsigns of the CW IDs that the radio sent.
func (b *bench) cwIDs() []string {
	var out []string
	for _, f := range b.sent {
		if f.Command == mmdvm.CommandSendCWID {
			out = append(out, string(f.Payload))
		}
	}
	return out
}

func TestCWIDGoesOutAQuarterOfTheTimeAfterStartAndThenEveryTime(t *testing.T) {
	t.Parallel()
	b := cwBench(t, true)
	b.run(15*time.Second - 100*time.Millisecond)
	if n := len(b.cwIDs()); n != 0 {
		t.Fatalf("%d CW IDs before a quarter of the time", n)
	}
	b.run(400 * time.Millisecond)
	if ids := b.cwIDs(); len(ids) != 1 || ids[0] != "KI5VMF" {
		t.Fatalf("CW IDs %q after a quarter of the time, want one of KI5VMF", ids)
	}
	b.run(59 * time.Second)
	if n := len(b.cwIDs()); n != 1 {
		t.Fatalf("%d CW IDs before the time", n)
	}
	b.run(2 * time.Second)
	if n := len(b.cwIDs()); n != 2 {
		t.Fatalf("%d CW IDs after the time, want 2", n)
	}
}

func TestCWIDWaitsForTheChannelToBeIdle(t *testing.T) {
	t.Parallel()
	b := cwBench(t, false)
	b.run(10 * time.Second)
	call := voiceCall(t, layer2.SyncSourceMS, 30)
	b.playRF(enums.Slot2, call)
	if n := len(b.cwIDs()); n != 0 {
		t.Fatalf("%d CW IDs during a call", n)
	}
	var idleAt time.Duration
	for b.now < 60*time.Second && idleAt == 0 {
		b.run(benchStep)
		if !b.r.keyed {
			idleAt = b.now
		}
	}
	if idleAt == 0 {
		t.Fatal("the hotspot did not go idle")
	}
	b.run(15*time.Second - 500*time.Millisecond)
	if n := len(b.cwIDs()); n != 0 {
		t.Fatalf("%d CW IDs within a quarter of the time after the call", n)
	}
	b.tx = true
	b.run(5 * time.Second)
	if n := len(b.cwIDs()); n != 0 {
		t.Fatalf("%d CW IDs while the modem transmits", n)
	}
	b.tx = false
	b.run(time.Second)
	if n := len(b.cwIDs()); n != 1 {
		t.Fatalf("%d CW IDs after the modem stopped transmitting, want 1", n)
	}
}

func TestNoCWIDWhenItIsOff(t *testing.T) {
	t.Parallel()
	b := newBench(t, true, 2)
	b.run(3 * time.Minute)
	if n := len(b.cwIDs()); n != 0 {
		t.Fatalf("%d CW IDs with the CW ID off", n)
	}
}
