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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
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
