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

package messages_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreGivesAnEarlyResponseToItsOwnMessageOnAnotherReplica(t *testing.T) {
	t.Parallel()
	one, two, database := storeReplicas(t)
	now := time.Now()
	h1, p1 := confirmed(t, "first", 1)
	h2, p2 := confirmed(t, "second", 2)
	one.DataPacket(&models.Call{ID: 1}, delivered(h1, p1, false), now)
	one.Flush()
	one.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(time.Second))
	one.Flush()
	two.DataResponse(responseRow(4), responseEvent(packet.ResponseNACKUndeliverable), now.Add(3500*time.Millisecond))
	two.Flush()
	one.DataPacket(&models.Call{ID: 3}, delivered(h2, p2, false), now.Add(3*time.Second))
	msgs := all(t, one, database)
	require.Len(t, msgs, 2)
	assert.Equal(t, models.CallResponseACK, msgs[0].Response, "the first message keeps its own ACK")
	assert.Equal(t, models.CallResponseNACK, msgs[1].Response, "the NACK heard first belongs to the second message")
}
