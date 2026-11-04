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
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoDataMessagesToOneTimeslotGoOneAfterTheOther(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		var wg sync.WaitGroup
		for _, body := range []string{"first", "second"} {
			var hd sms.Header
			payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(answerService), Destination: uint32(answerRadio), Body: []byte(body)}, &hd)
			require.NoError(t, err)
			m := data.Message{Header: hd.Data, Rate: enums.DataTypeRate34, Payload: payload}
			wg.Go(func() {
				res, err := h.SendData(context.Background(), answerRepeater, true, m)
				assert.NoError(t, err)
				assert.Equal(t, data.ResultSent, res)
			})
		}
		wg.Wait()
		synctest.Wait()
		_, delivered := answered(t, srv)
		require.Len(t, delivered, 2, "both arrive whole, not mixed on the slot")
		bodies := make([]string, 0, len(delivered))
		for _, m := range delivered {
			body, _, err := sms.Decode(nil, &m.Header, m.Payload)
			require.NoError(t, err)
			bodies = append(bodies, string(body))
		}
		assert.ElementsMatch(t, []string{"first", "second"}, bodies)
	})
}

type stamped struct {
	at time.Time
	p  models.Packet
}

// stamp reads what srv gets, with the time of the bubble each came.
func stamp(srv *hub.ServerHandle, out chan<- stamped, done <-chan struct{}) {
	for {
		select {
		case rp := <-srv.Packets:
			out <- stamped{at: time.Now(), p: rp.Packet}
		case <-done:
			return
		}
	}
}

// TestADataMessageWaitsForTheVoiceCallOnItsTimeslot routes a group call to
// timeslot 1 of a repeater and sends a text there while it is on the air:
// the text starts only once the timeslot carried no burst for a second.
func TestADataMessageWaitsForTheVoiceCallOnItsTimeslot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		got := make(chan stamped, 256)
		done := make(chan struct{})
		defer close(done)
		go stamp(srv, got, done)

		var hd sms.Header
		payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(answerService), Destination: uint32(answerRadio), Body: []byte("status")}, &hd)
		require.NoError(t, err)
		m := data.Message{Header: hd.Data, Rate: enums.DataTypeRate12, Payload: payload}

		call := make([]models.Packet, 0, 6)
		for i := range 5 {
			p := makeVoicePacket(weatherTG, 7301, true, false)
			p.Repeater, p.Seq = 100003, uint(i)
			call = append(call, p)
		}
		end := makeVoiceTermPacket(weatherTG, 7301, true, false)
		end.Repeater, end.Seq = 100003, 5
		call = append(call, end)

		h.RoutePacket(context.Background(), call[0], models.RepeaterTypeMMDVM)
		time.Sleep(60 * time.Millisecond)
		var wg sync.WaitGroup
		wg.Go(func() {
			res, err := h.SendData(context.Background(), answerRepeater, false, m)
			assert.NoError(t, err)
			assert.Equal(t, data.ResultSent, res)
		})
		for _, p := range call[1:] {
			h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
			time.Sleep(60 * time.Millisecond)
		}
		wg.Wait()
		synctest.Wait()

		var lastVoice, firstData time.Time
		for len(got) > 0 {
			s := <-got
			switch {
			case s.p.Src == answerService && firstData.IsZero():
				firstData = s.at
			case s.p.Src != answerService:
				require.True(t, firstData.IsZero(), "a data burst went out amid the call")
				lastVoice = s.at
			}
		}
		require.False(t, lastVoice.IsZero(), "the call reached the repeater")
		require.False(t, firstData.IsZero(), "the text went out")
		assert.GreaterOrEqual(t, firstData.Sub(lastVoice), time.Second, "the text waits for the hang after the call")
	})
}
