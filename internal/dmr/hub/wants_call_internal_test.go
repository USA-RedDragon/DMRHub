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

package hub

import (
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
)

func TestWantsCallKeepsPrivateCallsPrivate(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		user := &models.User{ID: 3002068, Repeaters: []models.Repeater{{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: 300206801},
			OwnerID:               3002068,
			TS1StaticTalkgroups:   []models.Talkgroup{{ID: 3002198}},
		}}}

		assert.True(t, wantsCall(user, &models.Call{GroupCall: true, DestinationID: 3002198}), "a group call on a talkgroup of the user's repeater")
		assert.False(t, wantsCall(user, &models.Call{GroupCall: false, SourceID: 3003442, DestinationID: 3002198}),
			"a private call to a radio whose ID is a talkgroup of the repeater is not the user's")
		assert.True(t, wantsCall(user, &models.Call{GroupCall: false, SourceID: 3003442, DestinationID: 3002068}))
		assert.True(t, wantsCall(user, &models.Call{GroupCall: false, SourceID: 3003442, DestinationID: 300206801}))
		assert.True(t, wantsCall(&models.User{ID: 3002068}, &models.Call{GroupCall: true, SourceID: 3002068, DestinationID: 91}),
			"a user without repeaters still sees their own calls")
		assert.False(t, wantsCall(user, &models.Call{GroupCall: true, DestinationID: 91}))
	})
}

func TestThroughRepeaterSendsTheCallsOfSysopRepeaters(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		user := &models.User{ID: 3009999, Repeaters: []models.Repeater{{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: 300999901},
			OwnerID:               3009999,
		}}}
		private := func(ingress uint) *models.Call {
			return &models.Call{GroupCall: false, SourceID: 3009998, DestinationID: 3009997, IngressID: ingress}
		}

		assert.False(t, wantsCall(user, private(300999901)), "a private call of others is not the user's own")
		assert.True(t, throughRepeater(user, private(300999901), nil), "the call came in through the user's repeater")
		assert.True(t, throughRepeater(user, private(300999902), []uint{300999902}), "the call came in through a co-sysop repeater")
		assert.False(t, throughRepeater(user, private(300999903), []uint{300999902}), "the call came in through another repeater")
		assert.False(t, throughRepeater(user, private(0), []uint{0}), "the call has no ingress repeater")
	})
}
