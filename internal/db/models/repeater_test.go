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

package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

const goodCallsign = "N0CALL"
const localhost = "127.0.0.1"

// --- WantRX / WantRXCall regression tests ---

func makeTestRepeater() models.Repeater {
	ts1DynID := uint(100)
	ts2DynID := uint(200)
	return models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 307201},
		OwnerID:               9999,
		TS1DynamicTalkgroupID: &ts1DynID,
		TS2DynamicTalkgroupID: &ts2DynID,
		TS1StaticTalkgroups:   []models.Talkgroup{{ID: 300}, {ID: 301}},
		TS2StaticTalkgroups:   []models.Talkgroup{{ID: 400}, {ID: 401}},
	}
}

func TestWantRX_MatchesRepeaterID(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: r.ID, Slot: true}
	want, slot := r.WantRX(pkt)
	if !want || slot != true {
		t.Errorf("expected (true, true), got (%v, %v)", want, slot)
	}
}

func TestWantRX_MatchesOwnerID(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: r.OwnerID, Slot: false}
	want, slot := r.WantRX(pkt)
	if !want || slot != false {
		t.Errorf("expected (true, false), got (%v, %v)", want, slot)
	}
}

func TestWantRX_MatchesTS2DynamicTalkgroup(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: 200, Slot: false}
	want, slot := r.WantRX(pkt)
	if !want || slot != true {
		t.Errorf("expected (true, true), got (%v, %v)", want, slot)
	}
}

func TestWantRX_MatchesTS1DynamicTalkgroup(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: 100, Slot: true}
	want, slot := r.WantRX(pkt)
	if !want || slot != false {
		t.Errorf("expected (true, false), got (%v, %v)", want, slot)
	}
}

func TestWantRX_MatchesTS2StaticTalkgroup(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: 400}
	want, slot := r.WantRX(pkt)
	if !want || slot != true {
		t.Errorf("expected (true, true), got (%v, %v)", want, slot)
	}
}

func TestWantRX_MatchesTS1StaticTalkgroup(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: 300}
	want, slot := r.WantRX(pkt)
	if !want || slot != false {
		t.Errorf("expected (true, false), got (%v, %v)", want, slot)
	}
}

func TestWantRX_NoMatch(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	pkt := models.Packet{Dst: 99999}
	want, _ := r.WantRX(pkt)
	if want {
		t.Error("expected want=false for unknown destination")
	}
}

func TestWantRX_NilDynamicTalkgroups(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	r.TS1DynamicTalkgroupID = nil
	r.TS2DynamicTalkgroupID = nil
	// Should still match static talkgroups
	pkt := models.Packet{Dst: 401}
	want, slot := r.WantRX(pkt)
	if !want || slot != true {
		t.Errorf("expected (true, true), got (%v, %v)", want, slot)
	}
}

func TestWantRXCall_MatchesSameAsWantRX(t *testing.T) {
	t.Parallel()
	r := makeTestRepeater()
	// Test that WantRXCall produces the same results as WantRX for equivalent inputs
	testCases := []struct {
		name string
		dst  uint
		slot bool
	}{
		{"repeater ID", r.ID, true},
		{"owner ID", r.OwnerID, false},
		{"TS2 dynamic", 200, false},
		{"TS1 dynamic", 100, true},
		{"TS2 static", 400, false},
		{"TS1 static", 301, true},
		{"no match", 99999, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkt := models.Packet{Dst: tc.dst, Slot: tc.slot}
			call := models.Call{DestinationID: tc.dst, TimeSlot: tc.slot}
			wantPkt, slotPkt := r.WantRX(pkt)
			wantCall, slotCall := r.WantRXCall(call)
			if wantPkt != wantCall || slotPkt != slotCall {
				t.Errorf("WantRX(%v,%v)=(%v,%v) != WantRXCall(%v,%v)=(%v,%v)",
					tc.dst, tc.slot, wantPkt, slotPkt,
					tc.dst, tc.slot, wantCall, slotCall)
			}
		})
	}
}

func FuzzRepeaterUnmarshalMsg(f *testing.F) {
	good := models.Repeater{}
	good.Connection = models.RepeaterStateConnected
	good.IP = localhost
	good.Port = 62031
	good.Salt = 0xDEADBEEF
	good.PingsReceived = 42
	good.Hotspot = true
	good.Callsign = goodCallsign
	good.ID = 307201
	encoded, err := good.MarshalMsg(nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte{})
	f.Add([]byte{0x80}) // minimal msgp map
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		var r models.Repeater
		_, _ = r.UnmarshalMsg(data)
	})
}

func BenchmarkRepeaterMarshalMsg(b *testing.B) {
	r := models.Repeater{}
	r.Connection = models.RepeaterStateConnected
	r.IP = localhost
	r.Port = 62031
	r.Salt = 0xDEADBEEF
	r.PingsReceived = 42
	r.Hotspot = true
	r.Callsign = goodCallsign
	r.ID = 307201
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.MarshalMsg(nil)
	}
}

func BenchmarkRepeaterUnmarshalMsg(b *testing.B) {
	r := models.Repeater{}
	r.Connection = models.RepeaterStateConnected
	r.IP = localhost
	r.Port = 62031
	r.Salt = 0xDEADBEEF
	r.PingsReceived = 42
	r.Hotspot = true
	r.Callsign = goodCallsign
	r.ID = 307201
	encoded, err := r.MarshalMsg(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dec models.Repeater
		_, _ = dec.UnmarshalMsg(encoded)
	}
}
