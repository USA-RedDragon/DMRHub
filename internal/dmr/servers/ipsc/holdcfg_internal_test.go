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
package ipsc

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configuredHangPacer(t *testing.T, callHold, dataHold time.Duration) (*pacer, *hangLog) {
	t.Helper()
	cfg := testConfig()
	cfg.DMR.IPSC.CallHangHold = callHold
	cfg.DMR.IPSC.DataHangHold = dataHold
	s := NewIPSCServer(cfg, nil, nil)
	t.Cleanup(s.pacer.stop)
	p, _, log, _ := newHangPacer(t)
	p.callHold, p.dataHold = s.pacer.callHold, s.pacer.dataHold
	return p, log
}

// TestTheCallHangHoldComesFromTheConfig sends a data call right after a
// voice call to a peer slot. The data call waits dmr.ipsc.call-hang-hold
// after the terminator, and not at all when it is 0.
func TestTheCallHangHoldComesFromTheConfig(t *testing.T) {
	t.Parallel()
	for name, hold := range map[string]time.Duration{"off": 0, "400ms": 400 * time.Millisecond} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, log := configuredHangPacer(t, hold, dataHangHold)
			k := slotKey{peer: 311860, slot: true}
			sendVoiceCall(p, k, 1, 'v')
			sent := receive(t, log, 3, time.Second)
			end := sent[2]
			require.Equal(t, byte('V'), end.tag)

			p.enqueue(k, 2, []byte{'d'}, false, dataSync(2, enums.DataTypeDataHeader), nil, time.Time{})
			got := log.next(t, time.Second)
			assert.Equal(t, byte('d'), got.tag)
			gap := got.at.Sub(end.at)
			assert.GreaterOrEqual(t, gap, hold)
			assert.Less(t, gap, hold+3*burstTime)
		})
	}
}

// TestTheDataHangHoldComesFromTheConfig sends an SMS and then a CSBK to a
// peer slot. The CSBK waits dmr.ipsc.data-hang-hold after the last block
// of the SMS, and not at all when it is 0.
func TestTheDataHangHoldComesFromTheConfig(t *testing.T) {
	t.Parallel()
	for name, hold := range map[string]time.Duration{"off": 0, "400ms": 400 * time.Millisecond} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, log := configuredHangPacer(t, callHangHold, hold)
			s := &IPSCServer{pacer: p, localID: 1}
			data := smsBursts(t, "one")
			pkts := make([]models.Packet, 0, len(data))
			for _, raw := range data {
				pkts = append(pkts, smsPacketOf(7, raw))
			}
			sendAll(s, pkts)
			s.sendTo(smsPeer, smsPacketOf(8, preambleBursts(t, 1, 0)[0]))

			got := receive(t, log, len(pkts), time.Second)
			next := log.next(t, callHang+time.Second)
			gap := next.at.Sub(got[len(got)-1].at)
			assert.GreaterOrEqual(t, gap, hold)
			assert.Less(t, gap, hold+3*burstTime)
		})
	}
}
