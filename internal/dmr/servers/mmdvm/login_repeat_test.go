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
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
)

// TestARepeatedRPTKDoesNotEndTheLogin logs a repeater in that sends its
// RPTK twice, as a repeater does when the RPTACK of the first one is late.
// The server acknowledges both, and in the MMDVM protocol an RPTACK does
// not say what it acknowledges, so the repeater takes the second one as
// the RPTACK of its config and goes on as logged in: it pings, sends its
// options and a call while the server still checks the config. The server
// must not refuse them, which would end the login of the repeater, and
// must answer its pings once the config is accepted.
func TestARepeatedRPTKDoesNotEndTheLogin(t *testing.T) {
	t.Parallel()
	const id = uint32(311960)
	d := dmrd.DialectMMDVM
	r := newDialectRig(t, uint(id))
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	k := r.expect(d, dmrd.CommandRPTACK)
	salt, ok := k.LoginSalt()
	require.True(t, ok)
	token, err := dmrdauth.Token(d, salt, []byte(testPassword))
	require.NoError(t, err)
	for range 2 {
		r.control(d, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
		r.expect(d, dmrd.CommandRPTACK)
	}

	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	require.True(t, r.s.records.Do(uint(id), func() { <-release }))
	r.send(rptc(t, d, id))
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTPING, ID: id})
	r.send(rpto(t, id, 0))
	r.send((&models.Packet{Signature: "DMRD", Repeater: uint(id), Src: 1000001, Dst: 1, GroupCall: true}).Encode())
	r.quiet(r.s)
	_, st, _ := r.link(r.s)
	require.Equal(t, dmrdsession.StateConfig, st, "the login of the repeater ended")

	free()
	r.expect(d, dmrd.CommandRPTACK)
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTPING, ID: id})
	r.expect(d, dmrd.CommandMSTPONG)
}
