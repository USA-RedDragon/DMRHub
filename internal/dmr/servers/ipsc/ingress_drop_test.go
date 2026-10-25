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

	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTranslateToMMDVMCountsABurstThatDoesNotEncode(t *testing.T) {
	t.Parallel()
	tr := newTestTranslator(t)
	u := ipscwire.User{ID: 99999, Source: 100, Destination: 200, CallControl: 0xBBBB, Opcode: ipscwire.OpGroupData, Priority: ipscPriorityData}
	u.RTP.Flags, u.RTP.PayloadType = rtpVersion2, rtpPayloadType
	u.Burst = ipscwire.BurstRaw
	u.Data = ipscwire.Payload{Status: 0x40, HasBody: true, Flags: [2]byte{0x80, 0x02}, TrailerLen: 4, Words: 17, Bits: 196}
	data, err := u.AppendBinary(nil)
	if err != nil {
		t.Fatal(err)
	}
	counter := metrics.NewMetrics().IngressDroppedBurstsTotal.WithLabelValues(metrics.DropReasonIPSCBurst)
	before := testutil.ToFloat64(counter)
	if got := tr.TranslateToMMDVM(byte(ipscwire.OpGroupData), data); len(got) != 0 {
		t.Fatalf("a raw block without a slot type made %d packets", len(got))
	}
	if after := testutil.ToFloat64(counter); after <= before {
		t.Fatalf("the raw block that did not encode as a DMRD burst was not counted: %s stayed at %v", metrics.DropReasonIPSCBurst, before)
	}
}
