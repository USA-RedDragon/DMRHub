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

package aprstext_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/plugins/aprstext"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	service = 4700
	userID  = 1000001
	radio2  = 100000101
)

type sentText struct {
	source, radio uint32
	body          string
}

// fakeHub is a hub that serves every feature, with the users of users.
type fakeHub struct {
	mu      sync.Mutex
	users   []*pluginv1.User
	formats map[uint32]error
	subs    []*pluginv1.Subscribe
	texts   []sentText
	aprs    []*pluginv1.APRSSend
	store   map[string][]byte
}

func newHub() *fakeHub {
	return &fakeHub{formats: map[uint32]error{}, store: map[string][]byte{}}
}

func (h *fakeHub) Has(pluginv1.Feature) bool { return true }

func (h *fakeHub) Subscribe(sub *pluginv1.Subscribe) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs = append(h.subs, sub)
	return nil
}

func (h *fakeHub) LookupRadio(_ context.Context, radio uint32) (*pluginv1.User, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range h.users {
		for _, r := range u.GetRadios() {
			if r == radio {
				return u, nil
			}
		}
	}
	return nil, plugin.ErrNotFound
}

func (h *fakeHub) LookupCallsign(_ context.Context, callsign string) (*pluginv1.User, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range h.users {
		if strings.EqualFold(u.GetCallsign(), callsign) {
			return u, nil
		}
	}
	return nil, plugin.ErrNotFound
}

func (h *fakeHub) SendText(_ context.Context, source, radio uint32, body string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.formats[radio]; err != nil {
		return "", err
	}
	h.texts = append(h.texts, sentText{source, radio, body})
	return "TMS", nil
}

func (h *fakeHub) SendAPRS(_ context.Context, m *pluginv1.APRSSend) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.aprs = append(h.aprs, m)
	return nil
}

func (h *fakeHub) StoreGet(_ context.Context, key string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.store[key]
	if !ok {
		return nil, plugin.ErrNotFound
	}
	return v, nil
}

func (h *fakeHub) StorePut(_ context.Context, key string, value []byte, _ time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store[key] = value
	return nil
}

func (h *fakeHub) sentAPRS() []*pluginv1.APRSSend {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*pluginv1.APRSSend(nil), h.aprs...)
}

func (h *fakeHub) sentTexts() []sentText {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]sentText(nil), h.texts...)
}

func (h *fakeHub) lastSub() *pluginv1.Subscribe {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subs[len(h.subs)-1]
}

func user(on bool, choice string, radios ...uint32) *pluginv1.User {
	return &pluginv1.User{
		UserId: userID, Callsign: "N1CALL", Radios: append([]uint32{userID}, radios...),
		Switches: []*pluginv1.UserSwitch{{UserId: userID, Callsign: "N1CALL", Key: aprstext.SwitchKey, On: on, Choice: choice}},
	}
}

// start runs the plugin on h, with the switch of N1CALL on as SSID 9 when
// on, and returns it with a func that hands it a message of the hub and
// waits until the plugin acted on it.
func start(t *testing.T, h *fakeHub, on bool) *aprstext.Service {
	t.Helper()
	h.users = append(h.users, user(on, "9", radio2))
	s := aprstext.New(t.Context(), h, []*pluginv1.IDRange{{First: service, Last: service}})
	require.NoError(t, s.Start())
	t.Cleanup(s.Stop)
	var switches []*pluginv1.UserSwitch
	if on {
		switches = h.users[0].GetSwitches()
	}
	s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitches{UserSwitches: &pluginv1.UserSwitches{Switches: switches}}})
	return s
}

func text(s *aprstext.Service, from uint32, body string) {
	s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Text{Text: &pluginv1.Text{
		Id: 1, Source: from, Destination: service, Slot: 2, Format: "TMS", Body: body, At: time.Now().UnixMilli(),
	}}})
	synctest.Wait()
}

func fromAPRS(s *aprstext.Service, m *pluginv1.APRSMessage) {
	s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Aprs{Aprs: m}})
	synctest.Wait()
}

func TestARadioTextGoesToAPRSFromTheUsersAddress(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		text(s, userID, "  n2call-7   Hello there ")
		assert.Equal(t, []*pluginv1.APRSSend{{Source: "N1CALL-9", Addressee: "N2CALL-7", Text: "Hello there", Number: "1"}}, h.sentAPRS())
		assert.Empty(t, h.sentTexts())
		text(s, radio2, "N2CALL Again")
		require.Len(t, h.sentAPRS(), 2)
		assert.Equal(t, "2", h.sentAPRS()[1].GetNumber(), "each message gets the next number")
		assert.Equal(t, "N2CALL", h.sentAPRS()[1].GetAddressee())
	})
}

func TestAMessageGoesAgainUntilItsAckComes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		text(s, userID, "N2CALL-7 Hello there")
		require.Len(t, h.sentAPRS(), 1)
		time.Sleep(aprstext.FirstRetry)
		synctest.Wait()
		require.Len(t, h.sentAPRS(), 2, "the first retry")
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "ack1"})
		assert.Zero(t, s.Pending())
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Len(t, h.sentAPRS(), 2, "no retry after the ack")
		assert.Empty(t, h.sentTexts(), "an ack does not go to the radio")
	})
}

func TestAMessageWithNoAckGoesFiveTimesWithLongerWaits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		begin := time.Now()
		var at []time.Duration
		text(s, userID, "N2CALL-7 Hello there")
		for range 20 {
			if n := len(h.sentAPRS()); n > len(at) {
				at = append(at, time.Since(begin))
			}
			time.Sleep(time.Second)
			synctest.Wait()
		}
		for len(at) < aprstext.Sends && time.Since(begin) < time.Hour {
			time.Sleep(time.Second)
			synctest.Wait()
			if len(h.sentAPRS()) > len(at) {
				at = append(at, time.Since(begin))
			}
		}
		assert.Equal(t, []time.Duration{0, 30 * time.Second, 90 * time.Second, 210 * time.Second, 450 * time.Second}, at)
		for _, m := range h.sentAPRS() {
			assert.Equal(t, "1", m.GetNumber(), "a retry keeps its number")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Len(t, h.sentAPRS(), aprstext.Sends)
		assert.Zero(t, s.Pending())
	})
}

func TestAReplyAckEndsTheRetries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		text(s, userID, "N2CALL-7 Hello there")
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "Hi back", Number: "AB}1"})
		assert.Zero(t, s.Pending())
		assert.Equal(t, []sentText{{service, userID, "N2CALL-7: Hi back"}}, h.sentTexts())
		assert.Equal(t, &pluginv1.APRSSend{Source: "N1CALL-9", Addressee: "N2CALL-7", Text: "ackAB}1"}, h.sentAPRS()[1], "the ack of a reply-ack copies its number")
	})
}

func TestARadioOfAUserWithTheSwitchOffOrOfNoUserGetsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, false)
		text(s, userID, "N2CALL-7 Hello there")
		text(s, userID, "not an address at all")
		text(s, 3000001, "N2CALL-7 Hello there")
		assert.Empty(t, h.sentAPRS())
		assert.Empty(t, h.sentTexts())
		assert.Empty(t, h.lastSub().GetAprs())
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "Hi", Number: "1"})
		assert.Empty(t, h.sentAPRS())
		assert.Empty(t, h.sentTexts())
	})
}

func TestATextThatIsNotAnAddressAndAMessageGetsTheInstructions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		for _, body := range []string{"Hello", "N2CALL-7", "N2CALL-7:Hi", "N2CALL-7890 Hi", "N2.CALL Hi"} {
			text(s, userID, body)
		}
		assert.Empty(t, h.sentAPRS())
		texts := h.sentTexts()
		require.Len(t, texts, 5)
		for _, got := range texts {
			assert.Equal(t, sentText{service, userID, aprstext.Usage}, got)
		}
	})
}

func TestALongTextGoesAsSeveralMessagesOfAtMost67Characters(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		words := strings.Repeat("abcdefghi ", 20)
		text(s, userID, "N2CALL-7 "+words)
		sent := h.sentAPRS()
		require.Len(t, sent, 4, "six words of ten characters fit in one message")
		all := make([]string, 0, len(sent))
		for i, m := range sent {
			assert.LessOrEqual(t, len(m.GetText()), aprstext.MaxText)
			assert.Equal(t, []string{"1", "2", "3", "4"}[i], m.GetNumber())
			all = append(all, m.GetText())
		}
		assert.Equal(t, strings.TrimSpace(words), strings.Join(all, " "))
	})
}

func TestTheTextLosesWhatAPRSCannotCarry(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a/b-c(d} ? e f", aprstext.Clean("a|b~c{d} é\r\ne\tf"))
	assert.Equal(t, []string{strings.Repeat("x", 67), "xxx"}, aprstext.Split(strings.Repeat("x", 70), 67))
}

func TestTheMessageNumbersGoOnFromTheStoreAndStartAgainAfter99999(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		h.store["number"] = []byte("99999")
		s := start(t, h, true)
		text(s, userID, "N2CALL-7 one")
		text(s, userID, "N2CALL-7 two")
		require.Len(t, h.sentAPRS(), 2)
		assert.Equal(t, "1", h.sentAPRS()[0].GetNumber())
		assert.Equal(t, "2", h.sentAPRS()[1].GetNumber())
		assert.Equal(t, []byte("2"), h.store["number"])
	})
}

func TestAnAPRSMessageGoesToTheRadioHeardLastAndIsAckedOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		assert.Equal(t, []string{"N1CALL-9"}, h.lastSub().GetAprs())
		at := time.Now().UnixMilli()
		s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Heard{Heard: &pluginv1.Heard{Radio: radio2, Slot: 2, At: &at}}})
		m := &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "See you at six", Number: "12"}
		fromAPRS(s, m)
		assert.Equal(t, []sentText{{service, radio2, "N2CALL-7: See you at six"}}, h.sentTexts())
		ack := &pluginv1.APRSSend{Source: "N1CALL-9", Addressee: "N2CALL-7", Text: "ack12"}
		assert.Equal(t, []*pluginv1.APRSSend{ack}, h.sentAPRS())
		fromAPRS(s, m)
		assert.Len(t, h.sentTexts(), 1, "a retry of a message that came does not go to the radio again")
		assert.Equal(t, []*pluginv1.APRSSend{ack, ack}, h.sentAPRS(), "a retry gets its ack again")
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "No number"})
		assert.Len(t, h.sentTexts(), 2)
		assert.Len(t, h.sentAPRS(), 2, "a message with no number gets no ack")
	})
}

func TestAnAPRSMessageSkipsARadioWithNoFormatAndIsNotAckedWhenTheRadioDoesNotAnswer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		h.formats[userID] = plugin.ErrNoFormat
		s := start(t, h, true)
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "Hi", Number: "1"})
		assert.Equal(t, []sentText{{service, radio2, "N2CALL-7: Hi"}}, h.sentTexts())
		assert.Len(t, h.sentAPRS(), 1)

		h.mu.Lock()
		h.formats[radio2] = plugin.ErrUnavailable
		h.mu.Unlock()
		fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: "N1CALL-9", Text: "Hi again", Number: "2"})
		assert.Len(t, h.sentTexts(), 1)
		assert.Len(t, h.sentAPRS(), 1, "no ack for a message that the radio did not get, so the sender sends it again")
	})
}

func TestAnAPRSMessageToAnotherAddressOrSSIDGoesNowhere(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		for _, to := range []string{"N1CALL-7", "N1CALL", "N9CALL-9"} {
			fromAPRS(s, &pluginv1.APRSMessage{Source: "N2CALL-7", Addressee: to, Text: "Hi", Number: "1"})
		}
		assert.Empty(t, h.sentTexts())
		assert.Empty(t, h.sentAPRS())
	})
}

func TestTheAddressesFollowTheSwitches(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		s := start(t, h, true)
		s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitch{UserSwitch: &pluginv1.UserSwitch{UserId: userID, Callsign: "N1CALL", Key: aprstext.SwitchKey, On: true, Choice: "5"}}})
		assert.Equal(t, []string{"N1CALL-5"}, h.lastSub().GetAprs())
		s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitch{UserSwitch: &pluginv1.UserSwitch{UserId: 1000002, Callsign: "N2CALL", Key: aprstext.SwitchKey, On: true}}})
		assert.Equal(t, []string{"N1CALL-5", "N2CALL-7"}, h.lastSub().GetAprs())
		s.Handle(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitch{UserSwitch: &pluginv1.UserSwitch{UserId: userID, Callsign: "N1CALL", Key: aprstext.SwitchKey}}})
		assert.Equal(t, []string{"N2CALL-7"}, h.lastSub().GetAprs())
	})
}
