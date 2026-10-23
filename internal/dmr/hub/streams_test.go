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

package hub_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
)

func TestAdmitStreamKeepsAStreamWithItsFirstSource(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	burst := func(stream, repeater uint) models.Packet {
		p := makeVoicePacket(1, stream, true, false)
		p.Repeater = repeater
		return p
	}

	assert.True(t, h.AdmitStream(burst(42, 100001), models.RepeaterTypeMMDVM))
	assert.True(t, h.AdmitStream(burst(42, 100001), models.RepeaterTypeMMDVM), "the source of the stream keeps it, also after a reconnect")
	assert.False(t, h.AdmitStream(burst(42, 100002), models.RepeaterTypeMMDVM), "another repeater with the same stream is a loop")
	assert.False(t, h.AdmitStream(burst(42, 100001), "openbridge"), "a peer with the same ID on another server is another source")
	assert.False(t, h.AdmitStream(burst(42, 100001), models.RepeaterTypeIPSC))
	assert.True(t, h.AdmitStream(burst(42, 100001), models.RepeaterTypeMMDVM), "dropped copies do not take the stream over")
	assert.True(t, h.AdmitStream(burst(43, 100002), models.RepeaterTypeMMDVM), "other streams are not affected")
	other := burst(42, 100003)
	other.Src = 1000002
	assert.True(t, h.AdmitStream(other, models.RepeaterTypeMMDVM), "another radio may use the same stream ID")
	assert.True(t, h.AdmitStream(burst(0, 100001), models.RepeaterTypeMMDVM))
	assert.True(t, h.AdmitStream(burst(0, 100002), models.RepeaterTypeMMDVM), "stream 0 carries no stream identity")
}

func TestASlotOfARepeaterIsCappedButPeersAreNot(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	h.LiftSlotRate(false)
	h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	admitted := func(server string, repeater uint, slot bool, n int) int {
		got := 0
		for i := range n {
			p := makeVoicePacket(1, uint(500+i), true, slot)
			p.Repeater = repeater
			if h.AdmitSlotRate(p, server) {
				got++
			}
		}
		return got
	}

	got := admitted(models.RepeaterTypeMMDVM, 100001, false, 200)
	assert.GreaterOrEqual(t, got, 50, "a slot may send in bursts")
	assert.LessOrEqual(t, got, 55, "200 bursts at once are far more than one slot can carry")
	assert.Equal(t, 50, admitted(models.RepeaterTypeMMDVM, 100001, true, 50), "the other slot has its own allowance")
	assert.Equal(t, 50, admitted(models.RepeaterTypeIPSC, 100001, false, 50), "the same ID on another server is another slot")
	assert.Equal(t, 2000, admitted("openbridge", 300001, false, 2000), "a peer carries many streams in bulk")
	time.Sleep(time.Second)
	assert.GreaterOrEqual(t, admitted(models.RepeaterTypeMMDVM, 100001, false, 40), 40, "the slot refills")
}

func TestSlotOverRateOnlyLooks(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	h.LiftSlotRate(false)
	h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	assert.False(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false), "a slot that sent nothing has its allowance")
	for i := range 60 {
		p := makeVoicePacket(1, uint(900+i), true, false)
		p.Repeater = 100001
		h.AdmitSlotRate(p, models.RepeaterTypeMMDVM)
	}
	assert.True(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false))
	assert.False(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, true), "the other slot is not over")
	assert.False(t, h.SlotOverRate("openbridge", 100001, false), "peers are never over")
	time.Sleep(100 * time.Millisecond)
	for range 100 {
		h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false)
	}
	p := makeVoicePacket(1, 999, true, false)
	p.Repeater = 100001
	assert.True(t, h.AdmitSlotRate(p, models.RepeaterTypeMMDVM), "looking takes none of the allowance")
}
