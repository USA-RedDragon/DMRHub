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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
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

// radioData routes, one slot apart from now, the bursts of an unconfirmed
// ETSI text from the radio to the plugin's ID on timeslot 1 of the
// repeater, which the hub does not answer, and returns when the last burst
// came. wait is the time.Sleep of the bubble of the test.
func radioData(t *testing.T, h *hub.Hub, stream uint, wait func(time.Duration)) time.Time {
	t.Helper()
	p := textPacket(t, &sms.Text{Format: sms.FormatIP, Source: uint32(answerRadio), Destination: uint32(answerService), Body: []byte("a text on the air")}, enums.DataTypeRate12)
	var last time.Time
	for i, b := range burstsOf(t, &p) {
		last = time.Now()
		h.RoutePacket(context.Background(), models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(i), Src: answerRadio, Dst: answerService, Repeater: answerRepeater,
			FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.SlotType().DataType), StreamID: stream, DMRData: b, BER: -1, RSSI: -1,
		}, models.RepeaterTypeMMDVM)
		wait(60 * time.Millisecond)
	}
	return last
}

// hubData returns the times of the bursts from the plugin's ID that srv
// got, and the texts that they carry to a radio.
func hubData(t *testing.T, got <-chan stamped) ([]time.Time, []string) {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(answerRadio), Source: layer2.SyncSourceMS, Lost: -1})
	require.NoError(t, err)
	var at []time.Time
	var texts []string
	var events []data.Event
	for i := 0; len(got) > 0; i++ {
		s := <-got
		require.Equal(t, answerService, s.p.Src, "only the text of the hub goes out")
		at = append(at, s.at)
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
	return at, texts
}

// TestADataMessageWaitsForTheDataOfARadioOnItsTimeslot sends a text to
// timeslot 1 of a repeater while a radio sends data there: the text starts
// only once the timeslot carried no burst for a second.
func TestADataMessageWaitsForTheDataOfARadioOnItsTimeslot(t *testing.T) {
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
		var wg sync.WaitGroup
		var last time.Time
		wg.Go(func() { last = radioData(t, h, 7601, time.Sleep) })
		time.Sleep(90 * time.Millisecond)
		wg.Go(func() {
			res, err := h.SendData(context.Background(), answerRepeater, false, m)
			assert.NoError(t, err)
			assert.Equal(t, data.ResultSent, res)
		})
		wg.Wait()
		synctest.Wait()

		at, texts := hubData(t, got)
		require.NotEmpty(t, at, "the text went out")
		assert.GreaterOrEqual(t, at[0].Sub(last), time.Second, "the text waits for the hang after the data of the radio")
		assert.Equal(t, []string{"status"}, texts)
	})
}

// TestARadioThatKeysUpStopsADataMessageThatGoesAgainAfterIt sends a long
// text to timeslot 1 of a repeater and routes data of a radio there while
// it goes out: the text stops at once, no burst of it goes out from the
// first burst of the radio until a second after its last, and then the
// text goes again in full.
func TestARadioThatKeysUpStopsADataMessageThatGoesAgainAfterIt(t *testing.T) {
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
		time.Sleep(330 * time.Millisecond)
		first := time.Now()
		last := radioData(t, h, 7602, time.Sleep)
		wg.Wait()
		synctest.Wait()

		at, texts := hubData(t, got)
		for _, a := range at {
			assert.False(t, a.After(first) && a.Before(last.Add(time.Second)), "a data burst at %s, during the data of the radio or its hang", a.Sub(first))
		}
		assert.Equal(t, []string{body}, texts, "the text in full, once")
	})
}

// TestTheResponseOfARadioDoesNotStopTheTryThatItAsksFor sends a confirmed
// text to a radio that misses a block of the first try: the response of
// the radio, which comes in on the timeslot, does not stop the try that it
// asks for, and the text arrives.
func TestTheResponseOfARadioDoesNotStopTheTryThatItAsksFor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(answerRadio), Respond: true, Source: layer2.SyncSourceMS, Lost: -1})
		require.NoError(t, err)
		start := time.Now()
		done := make(chan struct{})
		radio := make(chan struct{})
		var responses, texts int
		var tries []uint
		go func() {
			defer close(radio)
			var events []data.Event
			bursts, seq := 0, uint(0)
			for {
				var rp hub.RoutedPacket
				select {
				case rp = <-srv.Packets:
				case <-done:
					return
				}
				at := time.Since(start)
				raw := layer2.RawBurst(rp.Packet.DMRData)
				if len(tries) == 0 || tries[len(tries)-1] != rp.Packet.StreamID {
					tries = append(tries, rp.Packet.StreamID)
				}
				if bursts++; bursts == 9 {
					for i := range 13 {
						raw[i] ^= 0xFF
					}
				}
				events = rx.Burst(at, &raw, layer2.Hint{Kind: layer2.KindData}, events[:0])
				for _, e := range events {
					switch e.Kind {
					case data.EventRespond:
						responses++
						p := models.Packet{
							Signature: string(dmrconst.CommandDMRD), Seq: seq, Src: answerRadio, Dst: answerService, Repeater: answerRepeater,
							FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(e.Burst.SlotType().DataType), StreamID: 7701, DMRData: e.Burst, BER: -1, RSSI: -1,
						}
						seq++
						time.AfterFunc(e.Due-at, func() { h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM) })
					case data.EventDelivered:
						texts++
					case data.EventCallStart, data.EventCallEnd, data.EventResponse, data.EventReassembled:
					}
				}
			}
		}()

		body := strings.Repeat("status ", 10)
		var hd sms.Header
		payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(answerService), Destination: uint32(answerRadio), Confirmed: true, Body: []byte(body)}, &hd)
		require.NoError(t, err)
		m := data.Message{Header: hd.Data, Rate: enums.DataTypeRate12, Payload: payload}
		res, err := h.SendData(context.Background(), answerRepeater, false, m)
		require.NoError(t, err)
		close(done)
		<-radio
		assert.Equal(t, data.ResultDelivered, res, "the radio got the text and said so")
		assert.Equal(t, 1, texts, "the text arrives")
		assert.GreaterOrEqual(t, responses, 2, "a response that asks for a retry, and the ACK")
		assert.Len(t, tries, 1, "one send, not stopped and sent again")
	})
}
