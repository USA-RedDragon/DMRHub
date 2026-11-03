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

package plugins_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type sentText struct {
	source, radio uint
	body          string
}

type fakeTexts struct {
	mu     sync.Mutex
	sent   []sentText
	format string
	err    error
	hold   chan struct{}
}

func (f *fakeTexts) SendText(_ context.Context, source, radio uint, body string) (string, error) {
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.sent = append(f.sent, sentText{source, radio, body})
	return f.format, nil
}

func (f *fakeTexts) all() []sentText {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentText(nil), f.sent...)
}

type fakeAPRS struct {
	mu   sync.Mutex
	sent []aprs.Message
	off  atomic.Bool
}

func (f *fakeAPRS) send(m aprs.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeAPRS) options(t *testing.T) plugins.APRS {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	limits, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = limits.Close() })
	return plugins.APRS{Send: f.send, On: func() bool { return !f.off.Load() }, Limits: limits}
}

func (f *fakeAPRS) all() []aprs.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]aprs.Message(nil), f.sent...)
}

// sdkRig returns a rig with a plugin at 4700 to 4709, connected and
// welcomed, which declared an APRS switch that the hub kept, and two
// approved users.
func sdkRig(t *testing.T, opts ...plugins.ServiceOption) (*rig, models.Plugin, *stream, *pluginv1.Welcome) {
	t.Helper()
	r := newRig(t, opts...)
	require.NoError(t, r.db.Create(&models.User{ID: 1000001, Callsign: "N1CALL", Username: "n1call", Approved: true}).Error)
	require.NoError(t, r.db.Create(&models.User{ID: 1000002, Callsign: "N2CALL", Username: "n2call", Approved: true}).Error)
	require.NoError(t, r.db.Create(&models.User{ID: 1000003, Callsign: "N6CALL", Username: "n6call"}).Error)
	require.NoError(t, r.db.Create(&models.UserRadioID{UserID: 1000001, RadioID: 1000009, VerifiedAt: time.Now()}).Error)
	p, token := r.add("APRS", rng(4700, 4709))
	s := r.connect(token)
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetWelcome())
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Hello{Hello: &pluginv1.Hello{Version: "1.0.0", Switches: []*pluginv1.Switch{{
		Key: "aprs", Title: "APRS messages", Description: "Send and get APRS text messages with your radio.",
		Details: []*pluginv1.SwitchDetail{{Label: "Your APRS address", Value: "{callsign}-{choice}"}},
		Choice:  &pluginv1.SwitchChoice{Label: "SSID", Options: []string{"7", "9"}},
	}}}}}))
	out := &stream{PluginService_ConnectClient: s, token: token}
	a := ask(t, out, &pluginv1.Request{Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: "none"}}})
	require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, a.GetCode(), "the hub read the hello before the request")
	return r, p, out, m.GetWelcome()
}

// stream is the stream of a plugin with the number of its last request.
type stream struct {
	pluginv1.PluginService_ConnectClient
	last  uint32
	token string
}

func ask(t *testing.T, s *stream, req *pluginv1.Request) *pluginv1.Answer {
	t.Helper()
	s.last++
	req.Id = s.last
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: req}}))
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetAnswer(), "got %v", m)
	require.Equal(t, req.GetId(), m.GetAnswer().GetId())
	return m.GetAnswer()
}

func switchOn(t *testing.T, r *rig, p models.Plugin, user uint, on bool, choice string) {
	t.Helper()
	bubbleWaitSwitches(t, r, p)
	u, err := models.SetUserPluginSwitch(r.db, p.ID, user, "aprs", on, choice, time.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishSwitch(r.ps, p.ID, &u))
}

func bubbleWaitSwitches(t *testing.T, r *rig, p models.Plugin) {
	t.Helper()
	bubble.SettleDB(t, r.db, time.Minute)
	specs, err := models.PluginSwitchesOf(r.db, p.ID)
	require.NoError(t, err)
	require.Len(t, specs, 1, "the hub did not keep the switch of the hello")
}

func TestAHubWelcomesAPluginWithTheFeaturesItServes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, _, _, w := sdkRig(t)
		assert.ElementsMatch(t, []pluginv1.Feature{
			pluginv1.Feature_FEATURE_STORE, pluginv1.Feature_FEATURE_LOOKUP, pluginv1.Feature_FEATURE_REPEATER,
			pluginv1.Feature_FEATURE_HEARD, pluginv1.Feature_FEATURE_TEXTS, pluginv1.Feature_FEATURE_SWITCHES,
		}, w.GetFeatures(), "no text or APRS sending without a sender")
		_, _, _, w = sdkRig(t, plugins.WithTexts(&fakeTexts{}), plugins.WithAPRS((&fakeAPRS{}).options(t)), plugins.WithAPRSInbox())
		assert.Contains(t, w.GetFeatures(), pluginv1.Feature_FEATURE_SEND_TEXT)
		assert.Contains(t, w.GetFeatures(), pluginv1.Feature_FEATURE_APRS)
		assert.Contains(t, w.GetFeatures(), pluginv1.Feature_FEATURE_APRS_INBOX)
	})
}

func TestAPluginKeepsValuesInItsStore(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, _, s, _ := sdkRig(t)
		ttl := uint32(60)
		a := ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StorePut{StorePut: &pluginv1.StorePut{Key: "mail/N2CALL/1", Value: []byte("hi"), TtlSeconds: &ttl}}})
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StorePut{StorePut: &pluginv1.StorePut{Key: "mail/N2CALL/2", Value: []byte("yo")}}}).GetCode())

		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: "mail/N2CALL/1"}}})
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
		assert.Equal(t, []byte("hi"), a.GetStoreValue().GetValue())
		assert.Equal(t, time.Now().Add(time.Minute).UnixMilli(), a.GetStoreValue().GetExpiresAt())

		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreList{StoreList: &pluginv1.StoreList{Prefix: "mail/", Limit: 1}}})
		require.Len(t, a.GetStoreEntries().GetEntries(), 1)
		assert.Equal(t, "mail/N2CALL/1", a.GetStoreEntries().GetEntries()[0].GetKey())
		assert.True(t, a.GetStoreEntries().GetMore())

		time.Sleep(time.Minute)
		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: "mail/N2CALL/1"}}})
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, a.GetCode(), "the key expired")

		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreDelete{StoreDelete: &pluginv1.StoreDelete{Key: "mail/N2CALL/2"}}}).GetCode())
		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreList{StoreList: &pluginv1.StoreList{}}})
		assert.Empty(t, a.GetStoreEntries().GetEntries())

		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StorePut{StorePut: &pluginv1.StorePut{Key: "big", Value: make([]byte, models.MaxPluginStoreValue+1)}}})
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, a.GetCode())
		a = ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{}}})
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, a.GetCode())
		a = ask(t, s, &pluginv1.Request{})
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, a.GetCode(), "a request the hub does not know")
	})
}

func TestAPluginLooksUpAUserByRadioOrCallsignWithItsSwitches(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, p, s, _ := sdkRig(t)
		switchOn(t, r, p, 1000001, true, "9")
		for _, l := range []*pluginv1.Lookup{{By: &pluginv1.Lookup_Radio{Radio: 1000009}}, {By: &pluginv1.Lookup_Callsign{Callsign: "n1call"}}} {
			a := ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Lookup{Lookup: l}})
			require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
			want := &pluginv1.User{UserId: 1000001, Callsign: "N1CALL", Radios: []uint32{1000001, 1000009},
				Switches: []*pluginv1.UserSwitch{{UserId: 1000001, Callsign: "N1CALL", Key: "aprs", On: true, Choice: "9"}}}
			assert.True(t, proto.Equal(want, a.GetUser()), "got %v", a.GetUser())
		}
		a := ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Lookup{Lookup: &pluginv1.Lookup{By: &pluginv1.Lookup_Callsign{Callsign: "N2CALL"}}}})
		require.Len(t, a.GetUser().GetSwitches(), 1)
		assert.False(t, a.GetUser().GetSwitches()[0].GetOn())
		assert.Equal(t, "7", a.GetUser().GetSwitches()[0].GetChoice(), "the first option is the preset")
		for _, l := range []*pluginv1.Lookup{{By: &pluginv1.Lookup_Radio{Radio: 1000003}}, {By: &pluginv1.Lookup_Callsign{Callsign: "N0BODY"}}, {By: &pluginv1.Lookup_Radio{Radio: 1234567}}} {
			assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Lookup{Lookup: l}}).GetCode(), l)
		}
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Lookup{Lookup: &pluginv1.Lookup{}}}).GetCode())
	})
}

func TestAPluginReadsTheTalkgroupsOfARepeater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, _, s, _ := sdkRig(t)
		for _, id := range []uint{3100, 91, 9} {
			require.NoError(t, r.db.Create(&models.Talkgroup{ID: id, Name: fmt.Sprint(id)}).Error)
		}
		dyn := uint(9)
		rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001, Callsign: "N5CALL"}, OwnerID: 1000001, TS2DynamicTalkgroupID: &dyn}
		require.NoError(t, r.db.Create(&rpt).Error)
		require.NoError(t, models.SetRepeaterStaticTalkgroups(r.db, 100001, []models.Talkgroup{{ID: 91}}, []models.Talkgroup{{ID: 3100}}))

		a := ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Repeater{Repeater: &pluginv1.RepeaterQuery{Repeater: 100001}}})
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
		nine := uint32(9)
		want := &pluginv1.RepeaterInfo{Repeater: 100001, Callsign: "N5CALL", Slots: []*pluginv1.SlotInfo{
			{Slot: 1, StaticTalkgroups: []uint32{91}},
			{Slot: 2, StaticTalkgroups: []uint32{3100}, DynamicTalkgroup: &nine},
		}}
		assert.True(t, proto.Equal(want, a.GetRepeater()), "got %v", a.GetRepeater())
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_Repeater{Repeater: &pluginv1.RepeaterQuery{Repeater: 1}}}).GetCode())
	})
}

func sendText(t *testing.T, s *stream, source, radio uint32, body string) *pluginv1.Answer {
	t.Helper()
	return ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{Source: source, Radio: radio, Body: body}}})
}

func TestAPluginSendsATextOnlyFromItsIDsAndOnlyInAFormatTheHubKnows(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		texts := &fakeTexts{format: "TMS"}
		r, _, s, _ := sdkRig(t, plugins.WithTexts(texts))
		_, other := r.add("Parrot", rng(9990, 9990))
		_ = other

		a := sendText(t, s, 4700, 1000001, "1 new message")
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
		assert.Equal(t, "TMS", a.GetTextSent().GetFormat())
		assert.Equal(t, []sentText{{4700, 1000001, "1 new message"}}, texts.all())

		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_REFUSED, sendText(t, s, 4710, 1000001, "hi").GetCode(), "not the plugin's ID")
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_REFUSED, sendText(t, s, 4700, 9990, "hi").GetCode(), "an ID of a plugin")
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, sendText(t, s, 4700, 1000001, "").GetCode())
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, sendText(t, s, 4700, 1<<24, "hi").GetCode())
		assert.Len(t, texts.all(), 1)

		texts.mu.Lock()
		texts.err = fmt.Errorf("the radio never sent a text: %w", plugins.ErrNoFormat)
		texts.mu.Unlock()
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NO_FORMAT, sendText(t, s, 4700, 1000002, "hi").GetCode())
		for err, code := range map[error]pluginv1.ErrorCode{
			plugins.ErrTextDoesNotFit: pluginv1.ErrorCode_ERROR_CODE_INVALID,
			plugins.ErrRadioRefused:   pluginv1.ErrorCode_ERROR_CODE_REFUSED,
			plugins.ErrNoAnswer:       pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE,
		} {
			texts.mu.Lock()
			texts.err = fmt.Errorf("sending: %w", err)
			texts.mu.Unlock()
			assert.Equal(t, code, sendText(t, s, 4700, 1000002, "hi").GetCode(), err)
		}
	})
}

func TestAHubWithoutATextSenderRefusesTexts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, _, s, _ := sdkRig(t)
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, sendText(t, s, 4700, 1000001, "hi").GetCode())
	})
}

func TestAPluginThatAsksTooMuchAtOnceIsToldToSlowDown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		texts := &fakeTexts{format: "TMS", hold: make(chan struct{})}
		_, _, s, _ := sdkRig(t, plugins.WithTexts(texts))
		for i := range 70 {
			req := &pluginv1.Request{Id: uint32(1000 + i), Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{Source: 4700, Radio: 1000001, Body: "hi"}}}
			require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: req}}))
		}
		m, err := recv(t, s)
		require.NoError(t, err)
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_RATE, m.GetAnswer().GetCode(), "got %v", m)
		assert.GreaterOrEqual(t, m.GetAnswer().GetId(), uint32(1000+64))
		close(texts.hold)
	})
}

func sendAPRS(t *testing.T, s *stream, source, text string) *pluginv1.Answer {
	t.Helper()
	return ask(t, s, &pluginv1.Request{Op: &pluginv1.Request_AprsSend{AprsSend: &pluginv1.APRSSend{Source: source, Addressee: "N2CALL-9", Text: text, Number: "1"}}})
}

func TestAPluginSendsAPRSOnlyAsAUserWhoTurnedItsSwitchOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		out := &fakeAPRS{}
		opts := out.options(t)
		r, p, s, _ := sdkRig(t, plugins.WithAPRS(opts))
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_REFUSED, sendAPRS(t, s, "N1CALL-7", "hi").GetCode(), "the switch is off")
		switchOn(t, r, p, 1000001, true, "")
		a := sendAPRS(t, s, "n1call-7", "Hello there")
		require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, a.GetCode(), a.GetError())
		assert.Equal(t, []aprs.Message{{Source: "N1CALL-7", Addressee: "N2CALL-9", Text: "Hello there", Number: "1"}}, out.all())
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_REFUSED, sendAPRS(t, s, "N2CALL-7", "hi").GetCode(), "another user")
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_INVALID, sendAPRS(t, s, "N1CALL-7", "a{b").GetCode())

		for range 29 {
			require.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, sendAPRS(t, s, "N1CALL-7", "hi").GetCode())
		}
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_RATE, sendAPRS(t, s, "N1CALL-7", "hi").GetCode())
		again := &stream{PluginService_ConnectClient: r.connect(s.token), token: s.token}
		m, err := recv(t, again)
		require.NoError(t, err)
		require.NotNil(t, m.GetWelcome())
		s = again
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_RATE, sendAPRS(t, s, "N1CALL-7", "hi").GetCode(), "the limit lasts over a new connection")
		time.Sleep(time.Minute)
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED, sendAPRS(t, s, "N1CALL-7", "hi").GetCode())
		assert.Len(t, out.all(), 31)

		out.off.Store(true)
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, sendAPRS(t, s, "N1CALL-7", "hi").GetCode(), "no APRS-IS login")
		out.off.Store(false)

		switchOn(t, r, p, 1000001, false, "")
		assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_REFUSED, sendAPRS(t, s, "N1CALL-7", "hi").GetCode())
	})
}

// TestTheHubNeverDropsAnAnswer fills the queue of a plugin that does not
// read with texts, so that the hub drops texts, and checks that the answer
// to a request still comes once the plugin reads.
func TestTheHubNeverDropsAnAnswer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, _, s, _ := sdkRig(t)
		subscribeTo(t, s, &pluginv1.Subscribe{Texts: true})
		body := strings.Repeat("x", 400)
		for batch := range 20 {
			for i := range 100 {
				plugins.PublishText(r.ps, text(uint(1+batch*100+i), 4700, body))
			}
			synctest.Wait()
		}
		req := &pluginv1.Request{Id: 77, Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: "k"}}}
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: req}}))
		synctest.Wait()
		for {
			m, err := recv(t, s)
			require.NoError(t, err)
			if a := m.GetAnswer(); a != nil {
				assert.Equal(t, uint32(77), a.GetId())
				assert.Equal(t, pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, a.GetCode())
				return
			}
		}
	})
}

func subscribeTo(t *testing.T, s pluginv1.PluginService_ConnectClient, sub *pluginv1.Subscribe) {
	t.Helper()
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: sub}}))
	synctest.Wait()
}

func TestAPluginHearsEveryRadioOnlyWhenItAsks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, _, s, _ := sdkRig(t)
		subscribeTo(t, s, &pluginv1.Subscribe{Heard: true})
		require.NoError(t, plugins.PublishHeard(r.ps, 1000002, 100001, 1, 1))
		m, err := recv(t, s)
		require.NoError(t, err)
		assert.True(t, proto.Equal(&pluginv1.Heard{Radio: 1000002, Slot: 1, ColorCode: 1}, m.GetHeard()), "got %v", m)

		subscribeTo(t, s, &pluginv1.Subscribe{})
		require.NoError(t, plugins.PublishHeard(r.ps, 1000002, 100001, 2, 1))
		subscribeTo(t, s, &pluginv1.Subscribe{Heard: true})
		require.NoError(t, plugins.PublishHeard(r.ps, 1000001, 100001, 2, 3))
		m, err = recv(t, s)
		require.NoError(t, err)
		assert.Equal(t, uint32(1000001), m.GetHeard().GetRadio(), "no Heard while it did not ask")
	})
}

func text(id uint, dst uint, body string) *models.Message {
	return &models.Message{ID: id, SourceID: 1000001, DestinationID: dst, Slot: 2, RepeaterID: 100001, Format: models.MessageFormatTMS,
		Text: &body, ChecksumOK: true, ReceivedAt: time.Now()}
}

func TestAPluginGetsEachTextToItsIDsOnceAsTheHubReadIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, _, s, _ := sdkRig(t)
		plugins.PublishText(r.ps, text(1, 4700, "before"))
		subscribeTo(t, s, &pluginv1.Subscribe{Texts: true})
		plugins.PublishText(r.ps, text(2, 9990, "to another ID"))
		bad := text(3, 4700, "bad")
		bad.BadCRC = true
		plugins.PublishText(r.ps, bad)
		plugins.PublishText(r.ps, text(4, 4701, "N2CALL hello"))
		plugins.PublishText(r.ps, text(4, 4701, "N2CALL hello"))
		plugins.PublishText(r.ps, text(5, 4700, "INBOX"))
		m, err := recv(t, s)
		require.NoError(t, err)
		want := &pluginv1.Text{Id: 4, Source: 1000001, Destination: 4701, Slot: 2, Repeater: 100001, Format: "TMS", Body: "N2CALL hello", At: time.Now().UnixMilli()}
		assert.True(t, proto.Equal(want, m.GetText()), "got %v", m)
		m, err = recv(t, s)
		require.NoError(t, err)
		assert.Equal(t, "INBOX", m.GetText().GetBody(), "each text once")
	})
}

func TestAPluginFollowsItsSwitchesAndTheHubKeepsThemFromItsHello(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, p, s, _ := sdkRig(t)
		switchOn(t, r, p, 1000001, true, "9")
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Switches: true}}}))
		m, err := recv(t, s)
		require.NoError(t, err)
		want := &pluginv1.UserSwitches{Switches: []*pluginv1.UserSwitch{{UserId: 1000001, Callsign: "N1CALL", Key: "aprs", On: true, Choice: "9"}}}
		assert.True(t, proto.Equal(want, m.GetUserSwitches()), "got %v", m)

		switchOn(t, r, p, 1000002, true, "")
		m, err = recv(t, s)
		require.NoError(t, err)
		assert.True(t, proto.Equal(&pluginv1.UserSwitch{UserId: 1000002, Callsign: "N2CALL", Key: "aprs", On: true, Choice: "7"}, m.GetUserSwitch()), "got %v", m)

		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Hello{Hello: &pluginv1.Hello{}}}))
		synctest.Wait()
		specs, err := models.PluginSwitchesOf(r.db, p.ID)
		require.NoError(t, err)
		assert.Empty(t, specs, "a hello with no switches drops them")
	})
}

func TestAPluginGetsAPRSMessagesOnlyToTheAddressesOfItsUsers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, p, s, _ := sdkRig(t)
		switchOn(t, r, p, 1000001, true, "")
		subscribeTo(t, s, &pluginv1.Subscribe{Aprs: []string{"n1call-7", "N2CALL-7"}})
		require.NoError(t, plugins.PublishAPRS(r.ps, &pluginv1.APRSMessage{Source: "N0CALL", Addressee: "N2CALL-7", Text: "not a user of the plugin"}))
		require.NoError(t, plugins.PublishAPRS(r.ps, &pluginv1.APRSMessage{Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "hello", Number: "3"}))
		m, err := recv(t, s)
		require.NoError(t, err)
		assert.True(t, proto.Equal(&pluginv1.APRSMessage{Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "hello", Number: "3"}, m.GetAprs()), "got %v", m)

		switchOn(t, r, p, 1000001, false, "")
		synctest.Wait()
		require.NoError(t, plugins.PublishAPRS(r.ps, &pluginv1.APRSMessage{Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "after the switch went off"}))
		subscribeTo(t, s, &pluginv1.Subscribe{Heard: true})
		require.NoError(t, plugins.PublishHeard(r.ps, 1000002, 100001, 1, 1))
		m, err = recv(t, s)
		require.NoError(t, err)
		assert.NotNil(t, m.GetHeard(), "no APRS message after its user turned the switch off: got %v", m)
	})
}
