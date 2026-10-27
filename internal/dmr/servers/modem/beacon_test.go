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

func beaconBench(t *testing.T) *bench {
	t.Helper()
	return newBenchWith(t, true, 2, rssiMap{}, func(m *config.Modem) {
		m.Beacons = config.ModemBeacons{Enabled: true, Interval: 60, Duration: 3}
	})
}

// keying returns the DMR_START frames that the radio sent, true for a key
// up.
func (b *bench) keying() []bool {
	var out []bool
	for _, f := range b.sent {
		if f.Command == mmdvm.CommandDMRStart {
			out = append(out, f.Payload[0] == 1)
		}
	}
	return out
}

func TestBeaconKeysUpForItsDurationEveryInterval(t *testing.T) {
	t.Parallel()
	b := beaconBench(t)
	b.run(60*time.Second - 100*time.Millisecond)
	if k := b.keying(); len(k) != 0 {
		t.Fatalf("keyed %v before the interval", k)
	}
	b.run(200 * time.Millisecond)
	if k := b.keying(); len(k) != 1 || !k[0] {
		t.Fatalf("keyed %v at the interval, want one key up", k)
	}
	c := b.control()
	if last := c[len(c)-2]; last.Command != mmdvm.CommandSetMode || last.Payload[0] != byte(mmdvm.ModeDMR) {
		t.Fatalf("the beacon did not set DMR mode before DMR_START: %v", last)
	}
	b.run(3*time.Second - 200*time.Millisecond)
	if k := b.keying(); len(k) != 1 {
		t.Fatalf("keyed %v before the end of the beacon", k)
	}
	b.run(200 * time.Millisecond)
	c = b.control()
	if k := b.keying(); len(k) != 2 || k[1] {
		t.Fatalf("keyed %v after the beacon, want a dekey", k)
	}
	if last := c[len(c)-1]; last.Command != mmdvm.CommandSetMode || last.Payload[0] != byte(mmdvm.ModeIdle) {
		t.Fatalf("the beacon did not end in idle mode: %v", last)
	}
	b.run(57 * time.Second)
	if k := b.keying(); len(k) != 3 || !k[2] {
		t.Fatalf("keyed %v at the second interval, want a second key up", k)
	}
	b.run(3 * time.Second)
	if k := b.keying(); len(k) != 4 || k[3] {
		t.Fatalf("keyed %v after the second beacon, want a second dekey", k)
	}
}

func TestBeaconHandsTheTransmitterToACall(t *testing.T) {
	t.Parallel()
	b := beaconBench(t)
	b.run(60*time.Second + 200*time.Millisecond)
	call := voiceCall(t, layer2.SyncSourceBS, 10)
	b.playNetwork(enums.Slot1, 0x4444, call)
	if k := b.keying(); len(k) != 1 || !k[0] {
		t.Fatalf("keyed %v during a call that came in a beacon, want the one key up of the beacon", k)
	}
	b.run(10 * time.Second)
	if n := len(b.bursts(mmdvm.CommandDMRData1)); n < len(call) {
		t.Fatalf("the repeater sent %d bursts of %d", n, len(call))
	}
	if k := b.keying(); len(k) != 2 || k[1] {
		t.Fatalf("keyed %v after the call, want one dekey", k)
	}
}

func TestBeaconWaitsForACallToEnd(t *testing.T) {
	t.Parallel()
	b := beaconBench(t)
	b.run(55 * time.Second)
	call := voiceCall(t, layer2.SyncSourceBS, 10)
	b.playNetwork(enums.Slot2, 0x5555, call)
	if k := b.keying(); len(k) != 1 {
		t.Fatalf("keyed %v during the call", k)
	}
	for b.now < 90*time.Second && b.r.keyed {
		b.run(benchStep)
	}
	b.run(time.Second)
	if k := b.keying(); len(k) != 3 || !k[0] || k[1] || !k[2] {
		t.Fatalf("keyed %v after the call, want the call, its dekey and the beacon", k)
	}
}
