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
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
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

// TestAVoiceCallStopsADataMessageThatGoesAgainAfterIt sends a long text to
// timeslot 1 of a repeater and routes a group call there while it goes
// out: the text stops at once, no data burst goes out from the first voice
// burst until a second after the terminator, and then the text goes again
// in full.
func TestAVoiceCallStopsADataMessageThatGoesAgainAfterIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		got := make(chan stamped, 1024)
		done := make(chan struct{})
		defer close(done)
		go stamp(srv, got, done)

		body := strings.Repeat("status ", 40)
		var hd sms.Header
		payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(answerService), Destination: uint32(answerRadio), Body: []byte(body)}, &hd)
		require.NoError(t, err)
		m := data.Message{Header: hd.Data, Rate: enums.DataTypeRate12, Payload: payload}
		var wg sync.WaitGroup
		wg.Go(func() {
			res, err := h.SendData(context.Background(), answerRepeater, false, m)
			assert.NoError(t, err)
			assert.Equal(t, data.ResultSent, res)
		})
		time.Sleep(1230 * time.Millisecond)
		for i := range 6 {
			p := makeVoicePacket(weatherTG, 7401, true, false)
			p.Repeater, p.Seq = 100003, uint(i)
			if i == 5 {
				p = makeVoiceTermPacket(weatherTG, 7401, true, false)
				p.Repeater, p.Seq = 100003, 5
			}
			h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
			time.Sleep(60 * time.Millisecond)
		}
		wg.Wait()
		synctest.Wait()

		rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(answerRadio), Source: layer2.SyncSourceMS, Lost: -1})
		require.NoError(t, err)
		var firstVoice, lastVoice time.Time
		var texts []string
		var events []data.Event
		for i := 0; len(got) > 0; i++ {
			s := <-got
			if s.p.Src != answerService {
				if firstVoice.IsZero() {
					firstVoice = s.at
				}
				lastVoice = s.at
				continue
			}
			if !firstVoice.IsZero() {
				assert.False(t, !s.at.Before(firstVoice) && s.at.Before(lastVoice.Add(time.Second)), "a data burst at %s, during the call or its hang", s.at.Sub(firstVoice))
			}
			raw := layer2.RawBurst(s.p.DMRData)
			events = rx.Burst(time.Duration(i)*60*time.Millisecond, &raw, layer2.Hint{Kind: layer2.KindData}, events[:0])
			for _, e := range events {
				if e.Kind == data.EventDelivered {
					b, _, err := sms.Decode(nil, &e.Message.Header, e.Message.Payload)
					require.NoError(t, err)
					texts = append(texts, string(b))
				}
			}
		}
		require.False(t, firstVoice.IsZero(), "the call reached the repeater")
		assert.Equal(t, []string{body}, texts, "the text in full, once")
	})
}

// TestAVoiceCallStopsAPrompt plays a prompt to timeslot 1 of a repeater and
// routes a group call there while it plays: the rest of the prompt is
// dropped, and the call goes out whole.
func TestAVoiceCallStopsAPrompt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		hear(t, h, answerRadio, answerRepeater, 50)
		time.Sleep(time.Minute)
		synctest.Wait()
		for len(srv.Packets) > 0 {
			<-srv.Packets
		}
		got := make(chan stamped, 1024)
		done := make(chan struct{})
		defer close(done)
		go stamp(srv, got, done)

		clips := []string{prompts.WordTimeslot, prompts.Digit(1), prompts.WordLinked, prompts.WordNone, prompts.WordDynamic, prompts.WordNone}
		require.NoError(t, h.PlayPrompt(context.Background(), answerService, answerRadio, false, clips, 0))
		time.Sleep(time.Second + 330*time.Millisecond)
		call := 0
		for i := range 6 {
			p := makeVoicePacket(weatherTG, 7501, true, false)
			p.Repeater, p.Seq = 100003, uint(i)
			if i == 5 {
				p = makeVoiceTermPacket(weatherTG, 7501, true, false)
				p.Repeater, p.Seq = 100003, 5
			}
			h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
			call++
			time.Sleep(60 * time.Millisecond)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		var firstVoice time.Time
		prompt, voice := 0, 0
		for len(got) > 0 {
			s := <-got
			if s.p.Src == answerService {
				prompt++
				assert.True(t, firstVoice.IsZero(), "a prompt burst after the call started")
				continue
			}
			voice++
			if firstVoice.IsZero() {
				firstVoice = s.at
			}
		}
		assert.Equal(t, call, voice, "the call goes out whole")
		assert.Positive(t, prompt, "the prompt started")
	})
}
