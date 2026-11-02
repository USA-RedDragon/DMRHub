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
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wsUndeliverableEntry struct {
	Sentence   string `json:"sentence"`
	Target     string `json:"target"`
	RepeaterID uint   `json:"repeater_id"`
	Callsign   string `json:"callsign"`
	Reason     string `json:"reason"`
}

// TestWebsocketShowsEachViewerOnlyTheirUndeliverable sends a group call
// that three repeaters did not get to the websocket of four signed-in
// viewers. The caller and an admin see every repeater, the owner of one of
// them sees only theirs, and another user whose repeater took the call sees
// none.
func TestWebsocketShowsEachViewerOnlyTheirUndeliverable(t *testing.T) {
	t.Parallel()
	const (
		callerID = 3002068
		ownerID  = 3004201
		otherID  = 3004401
		adminID  = 3004501
		tg       = 91
	)
	all := []wsUndeliverableEntry{
		{Sentence: "IPSC repeater 990626 was busy on that slot.", Target: "ipsc", RepeaterID: 990626, Callsign: "W1XPR", Reason: "slot_busy"},
		{Sentence: "IPSC repeater 300344200 was busy on that slot.", Target: "ipsc", RepeaterID: 300344200, Callsign: "K3CALL", Reason: "slot_busy"},
		{Sentence: "IPSC repeater 300345510 cannot carry this kind of burst.", Target: "ipsc", RepeaterID: 300345510, Reason: "no_ipsc_form"},
	}
	viewers := []struct {
		name  string
		id    uint
		want  []wsUndeliverableEntry
		scope string
	}{
		{"another user", otherID, nil, ""},
		{"the repeater owner", ownerID, all[:1], "own"},
		{"the caller", callerID, all, "all"},
		{"an admin", adminID, all, "all"},
	}
	for _, v := range viewers {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h, database, ps, listening := makeTestHubWatching(t)
				seedTalkgroup(t, database, tg, "TG91")
				for id, call := range map[uint]string{callerID: "N6CALL", ownerID: "W1OWN", otherID: "W1CALL", otherID + 1: "K5OWN", adminID: "K4CALL"} {
					seedUser(t, database, id, call)
				}
				require.NoError(t, database.Model(&models.User{}).Where("id = ?", adminID).Update("admin", true).Error)
				tgID := uint(tg)
				for _, r := range []models.Repeater{
					{RepeaterConfiguration: models.RepeaterConfiguration{ID: 990626, Callsign: "W1XPR"}, OwnerID: ownerID},
					{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300344200, Callsign: "K3CALL"}, OwnerID: otherID + 1},
					{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300344201, Callsign: "W1CALL"}, OwnerID: otherID},
					{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300344202, Callsign: "K4CALL"}, OwnerID: adminID},
				} {
					r.Type = models.RepeaterTypeIPSC
					r.TS1DynamicTalkgroupID = &tgID
					require.NoError(t, database.Create(&r).Error)
				}

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				sub := ps.Subscribe(fmt.Sprintf("calls:%d", v.id))
				defer func() { _ = sub.Close() }()
				go h.ListenForWebsocket(ctx, v.id)
				bubble.Receive(t, listening, 10*time.Second, "ListenForWebsocket never subscribed to calls")

				sentences := make([]string, 0, len(all))
				for _, e := range all {
					sentences = append(sentences, e.Sentence)
				}
				callJSON, err := json.Marshal(models.Call{
					ID: 1, SourceID: callerID, User: models.User{ID: callerID, Callsign: "N6CALL"},
					GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tgID, DestinationID: tg,
					Undeliverable: sentences,
				})
				require.NoError(t, err)
				require.NoError(t, ps.Publish("calls", callJSON))

				raw := bubble.Receive(t, sub.Channel(), 5*time.Second, "the call did not reach the websocket")
				var row struct {
					Undeliverable []json.RawMessage `json:"undeliverable"`
					Scope         string            `json:"undeliverable_scope"`
				}
				require.NoError(t, json.Unmarshal(raw, &row))
				require.Len(t, row.Undeliverable, len(v.want), "%s sees %d undeliverable entries", v.name, len(row.Undeliverable))
				assert.Equal(t, v.scope, row.Scope)
				if len(v.want) == 0 {
					return
				}
				got := make([]wsUndeliverableEntry, 0, len(row.Undeliverable))
				for _, r := range row.Undeliverable {
					var e wsUndeliverableEntry
					require.NoError(t, json.Unmarshal(r, &e), "entry %s", r)
					got = append(got, e)
				}
				assert.Equal(t, v.want, got)
			})
		})
	}
}
