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

package mailbox_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/plugins/mailbox"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	box      = 4321
	alice    = 1000001
	aliceHT  = 1000009
	bob      = 1000002
	stranger = 1000077
)

type sentText struct {
	source, radio uint32
	body          string
}

type sentEmail struct {
	user         uint32
	key, subject string
	body         string
}

// fakeHub is a hub with a store in memory, two users, N1CALL with the
// radios alice and aliceHT and N2CALL with bob, and no store limit but
// full.
type fakeHub struct {
	store   map[string][]byte
	ttls    map[string]time.Duration
	users   []*pluginv1.User
	texts   []sentText
	emails  []sentEmail
	full    bool
	noText  map[uint32]bool
	ids     uint64
	replied []uint64
}

func newHub() *fakeHub {
	return &fakeHub{
		store: map[string][]byte{}, ttls: map[string]time.Duration{}, noText: map[uint32]bool{},
		users: []*pluginv1.User{
			{UserId: alice, Callsign: "N1CALL", Radios: []uint32{alice, aliceHT}},
			{UserId: bob, Callsign: "N2CALL", Radios: []uint32{bob}},
		},
	}
}

func (f *fakeHub) StoreGet(_ context.Context, key string) ([]byte, error) {
	v, ok := f.store[key]
	if !ok {
		return nil, &plugin.AnswerError{Code: pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return v, nil
}

func (f *fakeHub) StorePut(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if f.full {
		return &plugin.AnswerError{Code: pluginv1.ErrorCode_ERROR_CODE_FULL}
	}
	f.store[key] = append([]byte(nil), value...)
	f.ttls[key] = ttl
	return nil
}

func (f *fakeHub) StoreDelete(_ context.Context, key string) error {
	delete(f.store, key)
	return nil
}

func (f *fakeHub) StoreList(_ context.Context, prefix, after string, limit int) ([]*pluginv1.StoreEntry, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var keys []string
	for k := range f.store {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}
	out := make([]*pluginv1.StoreEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, &pluginv1.StoreEntry{Key: k, Value: f.store[k]})
	}
	return out, more, nil
}

func (f *fakeHub) LookupRadio(_ context.Context, radio uint32) (*pluginv1.User, error) {
	for _, u := range f.users {
		if slices.Contains(u.GetRadios(), radio) {
			return u, nil
		}
	}
	return nil, &plugin.AnswerError{Code: pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (f *fakeHub) LookupCallsign(_ context.Context, callsign string) (*pluginv1.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.GetCallsign(), callsign) {
			return u, nil
		}
	}
	return nil, &plugin.AnswerError{Code: pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (f *fakeHub) SendText(_ context.Context, source, radio uint32, body string) (string, error) {
	if f.noText[radio] {
		return "", &plugin.AnswerError{Code: pluginv1.ErrorCode_ERROR_CODE_NO_FORMAT}
	}
	f.texts = append(f.texts, sentText{source, radio, body})
	return "TMS", nil
}

func (f *fakeHub) ReplyText(ctx context.Context, source uint32, text *pluginv1.Text, body string) (string, error) {
	f.replied = append(f.replied, text.GetId())
	return f.SendText(ctx, source, text.GetSource(), body)
}

func (f *fakeHub) SendEmail(_ context.Context, userID uint32, switchKey, subject, body string) error {
	f.emails = append(f.emails, sentEmail{userID, switchKey, subject, body})
	return nil
}

func (f *fakeHub) emailOn(user uint32, on bool) {
	for _, u := range f.users {
		if u.GetUserId() == user {
			u.Switches = []*pluginv1.UserSwitch{{UserId: user, Callsign: u.GetCallsign(), Key: mailbox.EmailSwitch, On: on}}
		}
	}
}

// last returns the body of the last text, and drops the texts.
func (f *fakeHub) last(t *testing.T) string {
	t.Helper()
	require.NotEmpty(t, f.texts, "the mailbox sent no text")
	body := f.texts[len(f.texts)-1].body
	f.texts = nil
	return body
}

// send gives m a text from radio to the mailbox, and returns the answer.
func send(t *testing.T, m *mailbox.Mailbox, h *fakeHub, radio uint32, body string) string {
	t.Helper()
	h.ids++
	m.Text(context.Background(), &pluginv1.Text{Id: h.ids, Source: radio, Destination: box, Slot: 2, Repeater: 100001, Format: "TMS", Body: body, At: time.Now().UnixMilli()})
	require.Len(t, h.texts, 1, "one answer to %q", body)
	got := h.texts[0]
	assert.Equal(t, sentText{box, radio, got.body}, got, "the answer goes from the mailbox ID to the radio")
	assert.Equal(t, []uint64{h.ids}, h.replied, "the answer is a reply to the text, in its format")
	h.texts, h.replied = nil, nil
	return got.body
}

func TestAMessageIsLeftReadAndDeleted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		m := mailbox.New(h, box)
		assert.Equal(t, "Saved for N1CALL.", send(t, m, h, bob, "n1call  Meet at the club at 7 tonight"))
		key := ""
		for k := range h.store {
			if strings.HasPrefix(k, "m/") {
				key = k
			}
		}
		require.NotEmpty(t, key)
		assert.Equal(t, mailbox.Keep, h.ttls[key], "a message lasts 30 days")

		assert.Equal(t, "1 message: 1 N2CALL Meet at the...", send(t, m, h, alice, "inbox"))
		now := time.Now().UTC().Format("01-02 15:04")
		assert.Equal(t, "1/1 N2CALL "+now+"Z: Meet at the club at 7 tonight", send(t, m, h, aliceHT, "GET 1"), "any radio of the user reads it")
		assert.Equal(t, "Deleted 1. 0 messages left.", send(t, m, h, alice, "Delete 1"))
		assert.Equal(t, "No messages for N1CALL.", send(t, m, h, alice, "INBOX"))
	})
}

func TestMessagesAreNumberedOldestFirstAndDeletedAll(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		m := mailbox.New(h, box)
		send(t, m, h, bob, "N1CALL first")
		time.Sleep(time.Minute)
		send(t, m, h, stranger, "N1CALL second one")
		time.Sleep(time.Minute)
		send(t, m, h, bob, "N1CALL third")
		assert.Equal(t, "3 messages: 1 N2CALL first 2 1000077 second one 3 N2CALL third", send(t, m, h, alice, "INBOX"), "a radio with no user signs with its ID")
		assert.Contains(t, send(t, m, h, alice, "GET 2"), "2/3 1000077 ")
		assert.Equal(t, "No message 4. You have 3 messages.", send(t, m, h, alice, "GET 4"))
		assert.Equal(t, "No message x. You have 3 messages.", send(t, m, h, alice, "DELETE x"))
		assert.Equal(t, "Deleted 2. 2 messages left.", send(t, m, h, alice, "DELETE 2"))
		assert.Contains(t, send(t, m, h, alice, "GET 2"), "2/2 N2CALL ")
		assert.Equal(t, "Deleted all 2 messages.", send(t, m, h, alice, "DELETE ALL"))
		assert.Equal(t, "No messages for N1CALL.", send(t, m, h, alice, "INBOX"))
	})
}

func TestAMessageToAnUnknownCallsignIsRefused(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	assert.Equal(t, "No user has callsign N0NONE on this network.", send(t, m, h, bob, "n0none hello"))
	assert.Empty(t, h.store)
}

func TestARadioWithNoUserHasNoMailbox(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	for _, cmd := range []string{"INBOX", "GET 1", "DELETE ALL"} {
		assert.Equal(t, "Radio 1000077 has no user on this network, so it has no mailbox.", send(t, m, h, stranger, cmd), cmd)
	}
}

func TestOtherTextsGetTheHelp(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	help := "Text CALLSIGN and a message to leave one, or INBOX, GET 1, DELETE 1 or DELETE ALL."
	for _, body := range []string{"", "  ", "help", "N1CALL", "INBOX now", "GET", "GET ALL", "DELETE", "DELETE 1 2"} {
		assert.Equal(t, help, send(t, m, h, bob, body), "%q", body)
	}
	assert.Empty(t, h.store)
}

func TestAGroupTextToTheMailboxGetsNoAnswer(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	m.Text(context.Background(), &pluginv1.Text{Id: 1, Source: bob, Destination: box, Group: true, Body: "N1CALL hi"})
	assert.Empty(t, h.texts)
	assert.Empty(t, h.store)
}

func TestTheMailboxKeepsItsLimits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		m := mailbox.New(h, box)
		assert.Equal(t, "Too long: a message has at most 120 characters.", send(t, m, h, bob, "N1CALL "+strings.Repeat("x", mailbox.MaxText+1)))
		assert.Equal(t, "Saved for N1CALL.", send(t, m, h, bob, "N1CALL "+strings.Repeat("é", mailbox.MaxText)), "120 characters, not bytes")
		for i := 1; i < mailbox.MaxMessages; i++ {
			time.Sleep(time.Second)
			require.Equal(t, "Saved for N1CALL.", send(t, m, h, bob, fmt.Sprintf("N1CALL message %d", i)))
		}
		assert.Equal(t, "The mailbox of N1CALL is full.", send(t, m, h, bob, "N1CALL one more"))
		inbox := send(t, m, h, alice, "INBOX")
		assert.LessOrEqual(t, len([]rune(inbox)), 160, "an answer fits one text")
		assert.True(t, strings.HasPrefix(inbox, "20 messages: 1 N2CALL "), inbox)
		assert.True(t, strings.HasSuffix(inbox, " more"), inbox)
		assert.Contains(t, send(t, m, h, alice, "GET 1"), strings.Repeat("é", mailbox.MaxText))

		h.full = true
		assert.Equal(t, "The mailbox is full. Try again later.", send(t, m, h, alice, "N2CALL hi"), "the store of the plugin is full")
	})
}

func TestTheRecipientGetsANoticeWhenItsRadioIsNextHeard(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHub()
		m := mailbox.New(h, box)
		m.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
		assert.Empty(t, h.texts, "no notice with no message")
		send(t, m, h, bob, "N1CALL one")
		send(t, m, h, bob, "N1CALL two")
		m.Heard(context.Background(), &pluginv1.Heard{Radio: bob, Slot: 2})
		assert.Empty(t, h.texts, "the sender gets no notice")

		h.noText[aliceHT] = true
		m.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
		assert.Empty(t, h.texts, "the hub knows no format of the radio yet")
		h.noText[aliceHT] = false
		m.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
		assert.Empty(t, h.texts, "it waits before it tries the radio again")
		time.Sleep(10 * time.Minute)
		m.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
		require.Len(t, h.texts, 1)
		assert.Equal(t, sentText{box, aliceHT, "2 new messages for N1CALL. Text INBOX to 4321."}, h.texts[0])
		h.texts = nil

		m.Heard(context.Background(), &pluginv1.Heard{Radio: alice, Slot: 1})
		m.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
		assert.Empty(t, h.texts, "one notice for the user's radios")

		send(t, m, h, bob, "N1CALL three")
		m.Heard(context.Background(), &pluginv1.Heard{Radio: alice, Slot: 1})
		assert.Equal(t, "1 new message for N1CALL. Text INBOX to 4321.", h.last(t))

		send(t, m, h, bob, "N1CALL four")
		send(t, m, h, alice, "INBOX")
		m.Heard(context.Background(), &pluginv1.Heard{Radio: alice, Slot: 1})
		assert.Empty(t, h.texts, "no notice once the user read the inbox")
	})
}

func TestTheNoticesLastOverARestart(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	send(t, m, h, bob, "N1CALL hello")
	again := mailbox.New(h, box)
	require.NoError(t, again.Load(context.Background()))
	again.Heard(context.Background(), &pluginv1.Heard{Radio: aliceHT, Slot: 2})
	assert.Equal(t, "1 new message for N1CALL. Text INBOX to 4321.", h.last(t))
	assert.Equal(t, "1 message: 1 N2CALL hello", send(t, again, h, alice, "INBOX"))
}

func TestARecipientWhoTurnedTheSwitchOnGetsOneEmailUntilTheNotice(t *testing.T) {
	t.Parallel()
	h := newHub()
	m := mailbox.New(h, box)
	send(t, m, h, bob, "N1CALL before the switch")
	assert.Empty(t, h.emails, "no email while the switch is off")
	send(t, m, h, alice, "INBOX")

	h.emailOn(alice, true)
	send(t, m, h, bob, "N1CALL one")
	send(t, m, h, bob, "N1CALL two")
	require.Len(t, h.emails, 1, "one email until the user reads the mail")
	assert.Equal(t, sentEmail{alice, mailbox.EmailSwitch, "New message for N1CALL",
		"N2CALL left a message for N1CALL in the mailbox.\n\nText INBOX to 4321 from your radio to read it.\n"}, h.emails[0])
	assert.NotContains(t, h.emails[0].body, "one", "the email has no text of the message")

	send(t, m, h, alice, "INBOX")
	send(t, m, h, bob, "N1CALL three")
	assert.Len(t, h.emails, 2, "a new email after the user read the mail")

	h.emailOn(alice, false)
	send(t, m, h, alice, "DELETE ALL")
	send(t, m, h, bob, "N1CALL four")
	assert.Len(t, h.emails, 2)
}
