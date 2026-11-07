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

package mmdvm

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (r *dialectRig) shownRepeaters() []uint {
	r.t.Helper()
	found, err := models.ConnectedRepeaterPositions(r.s.DB, 0, true, r.clock.Now())
	require.NoError(r.t, err)
	ids := make([]uint, 0, len(found))
	for _, rpt := range found {
		ids = append(ids, rpt.ID)
	}
	return ids
}

func (r *dialectRig) row() *models.Repeater {
	r.t.Helper()
	var row models.Repeater
	require.NoError(r.t, r.s.DB.First(&row, homeRepeaterID).Error)
	return &row
}

// TestARepeaterLeavesThePositionsAtOnceWhenItsSessionEnds ends the session
// of a repeater that reports a position with RPTCL and by timeout, on a
// test clock. Right after the end the repeater is neither Online nor among
// the positions, and a new login shows it again. A timeout ends a session
// RepeaterOnline after its last ping, so only RPTCL needs the record of
// the disconnect to leave at once; the record is kept for both.
//
// The ticker of the server reads the clock while Advance runs the beacon
// at a minute after the login. The timeout case pings a second after the
// login, so that the session times out only at the end of the advance and
// the beacon goes out before it.
func TestARepeaterLeavesThePositionsAtOnceWhenItsSessionEnds(t *testing.T) {
	t.Parallel()
	for name, end := range map[string]func(r *dialectRig, fake *clock.Fake, id uint32){
		"RPTCL": func(r *dialectRig, _ *clock.Fake, id uint32) {
			r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTCL, ID: id})
			r.handled(r.s)
		},
		"timeout": func(r *dialectRig, fake *clock.Fake, id uint32) {
			fake.Advance(time.Second)
			r.ping(id)
			r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTPONG)
			r.handled(r.s)
			fake.Advance(dmrdsession.TimeoutDefault)
			r.expect(dmrd.DialectMMDVM, dmrd.CommandRPTSBKN)
			r.s.tick(context.Background())
			r.handled(r.s)
			r.quiet(r.s)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const id = homeRepeaterID
			fake := clock.NewFake(time.Now())
			r := newRig(t, []uint{id}, withClock(fake))
			r.login(dmrd.DialectMMDVM, id)
			r.homePosition("+30.2669-097.7729")
			require.Equal(t, []uint{id}, r.shownRepeaters())
			require.True(t, r.row().Online(fake.Now()))
			require.Nil(t, r.row().Disconnected)

			end(r, fake, id)
			assert.Empty(t, r.shownRepeaters())
			row := r.row()
			assert.False(t, row.Online(fake.Now()))
			require.NotNil(t, row.Disconnected, "the disconnect is recorded")
			assert.True(t, row.Disconnected.Equal(fake.Now()))

			fake.Advance(time.Second)
			r.login(dmrd.DialectMMDVM, id)
			r.homePosition("+30.2669-097.7729")
			assert.Equal(t, []uint{id}, r.shownRepeaters(), "a new login shows it again")
			assert.True(t, r.row().Online(fake.Now()))
		})
	}
}
