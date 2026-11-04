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

package positions_test

import (
	"context"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inbox is the callsigns that a test opts in, and the messages that the
// gateway delivers.
type inbox struct {
	mu        sync.Mutex
	callsigns []string
	got       chan aprs.Message
}

func (in *inbox) set(calls ...string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.callsigns = calls
}

func (in *inbox) list(context.Context) ([]string, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]string(nil), in.callsigns...), nil
}

func (g *gateway) startInbox(calls ...string) (*inbox, *watch) {
	g.t.Helper()
	in := &inbox{callsigns: calls, got: make(chan aprs.Message, 16)}
	_, w := g.openWith(func(gw *positions.Gateway) {
		gw.Inbox(in.list, func(m aprs.Message) { in.got <- m })
	})
	require.True(g.t, next(g.t, w.logins, "login reply"), "the server did not verify the login")
	return in, w
}

func TestTheGatewayFiltersForTheOptedInCallsignsAndDeliversTheirMessages(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	in, _ := g.startInbox("N1CALL", "n2call")
	assert.Equal(t, []string{"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3 filter g/N1CALL/N1CALL-*/N2CALL/N2CALL-*"}, g.srv.Logins())

	g.srv.Push("N9CALL-9>APDR16,TCPIP*,qAC,T2TEST::N3CALL-7 :not for us{1")
	g.srv.Push("N9CALL-9>APDR16,TCPIP*,qAC,T2TEST:!3016.03N/09744.59W>")
	g.srv.Push("N9CALL-9>APDR16,TCPIP*,qAC,T2TEST::N2CALL-7 :Hello there{2")
	assert.Equal(t, aprs.Message{Source: "N9CALL-9", Addressee: "N2CALL-7", Text: "Hello there", Number: "2"}, next(t, in.got, "message"))
	assert.Empty(t, in.got, "the gateway delivered a message to a callsign that did not opt in")
}

func TestTheGatewayLogsInAgainWhenTheOptedInCallsignsChange(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	in, w := g.startInbox()
	assert.Equal(t, []string{"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3"}, g.srv.Logins(), "no filter while no user opted in")
	in.set("N1CALL")
	require.True(t, next(t, w.logins, "login reply with the filter"))
	in.set()
	require.True(t, next(t, w.logins, "login reply without the filter"))
	assert.Equal(t, []string{
		"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3",
		"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3 filter g/N1CALL/N1CALL-*",
		"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3",
	}, g.srv.Logins())
}

func TestAPluginsMessageToAnOptedInCallsignComesBackToTheInbox(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	in, w := g.startInbox("N1CALL")
	sent := aprs.Message{Source: "N2CALL-7", Addressee: "N1CALL-7", Text: "Hello there", Number: "3"}
	require.NoError(t, positions.PublishMessage(g.ps, sent))
	assert.True(t, next(t, w.messages, "message"))
	assert.Equal(t, sent, next(t, in.got, "message"), "APRS-IS does not send it back to the login")
	require.NoError(t, positions.PublishMessage(g.ps, aprs.Message{Source: "N1CALL-7", Addressee: "N3CALL-7", Text: "Hi"}))
	assert.True(t, next(t, w.messages, "message"))
	assert.Equal(t, 2, len(g.lines(2)))
	assert.Empty(t, in.got, "the gateway delivered a message to a callsign that did not opt in")
}
