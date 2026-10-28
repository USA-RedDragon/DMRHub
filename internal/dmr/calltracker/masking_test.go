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

package calltracker_test

import (
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskedSMSHidesItsResponseAndDuration(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		store := messages.NewStore(database, memKV(t))
		ct.SetDataObserver(store)
		t.Cleanup(store.Stop)

		bursts := smsBursts(t, &sms.Text{Format: sms.FormatTMS, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte("secret")})
		newSender(t, ct, userB, userA, false, 7101).data(bursts...)
		newSender(t, ct, userA, userB, false, 7102).data(confirmedACK(t, bursts)...)
		calls := waitDone(t, ct, database, 2)
		store.Flush()

		var viewer models.User
		require.NoError(t, database.First(&viewer, userB).Error)
		require.False(t, viewer.SMSStorageEnabled)
		rows, err := apimodels.NewWSCallResponsesFor(database, &viewer, models.CurrentSMSWindow(), calls)
		require.NoError(t, err)
		msg, resp := rows[0], rows[1]
		assert.Empty(t, msg.SubKind, "the SMS row is masked")
		assert.Zero(t, msg.Duration, "the duration of a masked row tells its size")
		assert.Empty(t, resp.SubKind, "the response row tells that the masked row was answered")
		assert.Equal(t, models.CallResponseNone, resp.Response, "the response row tells the response of the masked SMS")
	})
}

type slowObserver struct {
	next  *messages.Store
	delay time.Duration
}

func (o slowObserver) DataPacket(call *models.Call, ev *data.Event, at time.Time) {
	c := *call
	e := *ev
	e.Message.Payload = append([]byte(nil), ev.Message.Payload...)
	go func() {
		time.Sleep(o.delay)
		o.next.DataPacket(&c, &e, at)
	}()
}

func (o slowObserver) DataResponse(call *models.Call, ev *data.Event, at time.Time) {
	o.next.DataResponse(call, ev, at)
}

func groupShortData(t *testing.T) []layer2.RawBurst {
	t.Helper()
	var h pdu.DataHeader
	require.NoError(t, h.SetUnconfirmed(pdu.UnconfirmedDataHeader{Group: true, SAP: enums.SAPShortData, LLIDDestination: uint32(tgID), LLIDSource: uint32(userB), FullMessageFlag: true}))
	p, err := packet.NewPacket(h, []byte("not an sms but a stored message"), enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

func TestPublicDataRowIsMaskedBeforeItsMessageIsStored(t *testing.T) {
	t.Parallel()
	for _, delay := range []time.Duration{0, 1500 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ct, database, ps := newTracker(t)
				store := messages.NewStore(database, memKV(t))
				if delay == 0 {
					ct.SetDataObserver(store)
				} else {
					ct.SetDataObserver(slowObserver{next: store, delay: delay})
				}
				t.Cleanup(store.Stop)
				sub := ps.Subscribe("calls:public")
				t.Cleanup(func() { _ = sub.Close() })

				newSender(t, ct, userB, tgID, true, 7103).data(groupShortData(t)...)
				c := waitDone(t, ct, database, 1)[0]
				bubble.Eventually(t, func() bool {
					store.Flush()
					var n int64
					require.NoError(t, database.Model(&models.Message{}).Where("transmission_id = ?", c.ID).Count(&n).Error)
					return n == 1
				}, 5*time.Second, 20*time.Millisecond, "the hub stores the message of the row")

				select {
				case raw := <-sub.Channel():
					var row map[string]any
					require.NoError(t, json.Unmarshal(raw, &row))
					assert.Nil(t, row["sap"], "delay %v: a visitor sees the SAP of a row that carried a stored message", delay)
					assert.InDelta(t, 0, row["octets"], 0, "delay %v: a visitor sees the size", delay)
				case <-time.After(5 * time.Second):
					t.Fatal("no public row")
				}

				rows, err := apimodels.NewWSCallResponsesFor(database, nil, models.CurrentSMSWindow(), []models.Call{c})
				require.NoError(t, err)
				assert.Nil(t, rows[0].SAP, "REST masks the row once the message is stored")
			})
		})
	}
}

func confirmedShortData(t *testing.T) []layer2.RawBurst {
	t.Helper()
	var h pdu.DataHeader
	require.NoError(t, h.SetConfirmed(pdu.ConfirmedDataHeader{ResponseRequested: true, SAP: enums.SAPShortData, LLIDDestination: uint32(userA), LLIDSource: uint32(userB), FullMessageFlag: true, SendSequenceNumber: 3}))
	p, err := packet.NewPacket(h, []byte("a stored message that is not an sms"), enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

func TestRetryRowOfAStoredMessageIsMasked(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		store := messages.NewStore(database, memKV(t))
		ct.SetDataObserver(store)
		t.Cleanup(store.Stop)

		bursts := confirmedShortData(t)
		newSender(t, ct, userB, userA, false, 7201).data(bursts...)
		waitDone(t, ct, database, 1)
		newSender(t, ct, userB, userA, false, 7202).data(bursts...)
		calls := waitDone(t, ct, database, 2)
		store.Flush()

		var viewer models.User
		require.NoError(t, database.First(&viewer, userA).Error)
		rows, err := apimodels.NewWSCallResponsesFor(database, &viewer, models.CurrentSMSWindow(), calls)
		require.NoError(t, err)
		for i, r := range rows {
			assert.Nil(t, r.SAP, "row %d of the stored message shows its SAP", i)
			assert.Zero(t, r.Octets, "row %d shows its size", i)
			assert.False(t, r.Confirmed, "row %d shows its confirmed flag", i)
		}
	})
}
