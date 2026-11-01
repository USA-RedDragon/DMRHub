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

package storeforward_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/storeforward"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	repeater      = 319186801
	otherRepeater = 310000101
	other         = 3100001
)

type sent struct {
	repeater uint
	slot     bool
	msg      data.Message
}

type fakeSender struct {
	mu      sync.Mutex
	sent    []sent
	results []data.Result
}

func (f *fakeSender) SendData(_ context.Context, repeater uint, slot bool, m data.Message) (data.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{repeater: repeater, slot: slot, msg: m})
	r := data.ResultSent
	if len(f.results) > 0 {
		r, f.results = f.results[0], f.results[1:]
	}
	return r, nil
}

func (f *fakeSender) LastBurst(uint) (time.Time, bool) { return time.Time{}, false }

func (f *fakeSender) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sent...)
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	for _, u := range []models.User{
		{ID: recipient, Callsign: "KI5VMF", Username: "recipient", Approved: true},
		{ID: sender, Callsign: "KD5SMP", Username: "sender", Approved: true},
		{ID: other, Callsign: "N0CALL", Username: "other", Approved: true},
	} {
		require.NoError(t, database.Create(&u).Error)
	}
	require.NoError(t, database.Create(&models.Repeater{ID: repeater, OwnerID: recipient, Callsign: "KI5VMF", Type: models.RepeaterTypeMMDVM}).Error)
	require.NoError(t, database.Create(&models.Repeater{ID: otherRepeater, OwnerID: other, Callsign: "N0CALL", Type: models.RepeaterTypeMMDVM}).Error)
	require.NoError(t, models.SetStoreForward(database, recipient, true, time.Now()))
	return database
}

func newService(t *testing.T, database *gorm.DB, cfg config.Messages) (*storeforward.Service, *fakeSender) {
	t.Helper()
	c := config.Config{}
	c.DMR.Messages = cfg
	s := storeforward.New(database, c, "replica-1")
	f := &fakeSender{}
	s.SetSender(f)
	t.Cleanup(s.Stop)
	return s, f
}

func text(t *testing.T, database *gorm.DB, src, dst uint, body string, at time.Time, edit func(*models.Message)) *models.Message {
	t.Helper()
	m := &models.Message{ReceivedAt: at, SourceID: src, DestinationID: dst, Format: models.MessageFormatTMS, ChecksumOK: true, Text: &body, StreamID: uint(at.UnixNano() & 0xffffff)}
	if edit != nil {
		edit(m)
	}
	require.NoError(t, database.Create(m).Error)
	return m
}

func radioSent(t *testing.T, database *gorm.DB, at time.Time) {
	t.Helper()
	radioSentOn(t, database, at, repeater)
}

func radioSentOn(t *testing.T, database *gorm.DB, at time.Time, on uint) {
	t.Helper()
	tpl := stored(t, airTexts(t, "anytone-dmr-text")[0].msg)
	tpl.ReceivedAt, tpl.Format, tpl.RepeaterID = at, models.MessageFormatAnyTone, on
	body := "DMR TEST"
	tpl.Text = &body
	require.NoError(t, database.Create(tpl).Error)
}

func held(t *testing.T, database *gorm.DB) []models.HeldMessage {
	t.Helper()
	var rows []models.HeldMessage
	require.NoError(t, database.Order("id").Find(&rows).Error)
	return rows
}

func TestAStreamTheHubCouldNotRouteIsHeld(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{})
	now := time.Now()
	m := text(t, database, sender, recipient, "are you there", now, nil)
	s.NotRouted(m.StreamID, recipient)
	assert.True(t, s.Hold(m, now, false))
	assert.False(t, s.Hold(m, now, false), "a text is held once")
	rows := held(t, database)
	require.Len(t, rows, 1)
	assert.Equal(t, m.ID, rows[0].MessageID)
	assert.Equal(t, uint(recipient), rows[0].RadioID)
	assert.Equal(t, uint(recipient), rows[0].UserID)
	assert.Equal(t, models.HeldStateHeld, rows[0].State)
}

func TestOnlyPrivateTextsToUsersWithTheSwitchOnAreHeld(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{})
	now := time.Now()
	for name, m := range map[string]*models.Message{
		"to a user with it off": text(t, database, recipient, other, "off", now, nil),
		"to a talkgroup":        text(t, database, sender, recipient, "group", now, func(m *models.Message) { m.IsGroup = true }),
		"not text":              text(t, database, sender, recipient, "data", now, func(m *models.Message) { m.Format = models.MessageFormatData }),
		"with a bad CRC":        text(t, database, sender, recipient, "bad", now, func(m *models.Message) { m.BadCRC = true }),
		"to a radio no one has": text(t, database, sender, 3199999, "nobody", now, nil),
	} {
		assert.False(t, s.Hold(m, now, false), name)
	}
	assert.Empty(t, held(t, database))
}

func TestAUserHoldsAtMostTheLimit(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{HoldMax: 2})
	now := time.Now()
	for i, want := range []bool{true, true, false} {
		m := text(t, database, sender, recipient, "n", now.Add(time.Duration(i)*time.Second), nil)
		assert.Equal(t, want, s.Hold(m, now, false), "text %d", i)
	}
}

func TestTheSweepHoldsAConfirmedTextWithoutAnAnswer(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{})
	now := time.Now()
	confirmed := func(r models.CallResponse) func(*models.Message) {
		return func(m *models.Message) { m.Confirmed, m.Response = true, r }
	}
	noAnswer := text(t, database, sender, recipient, "no answer", now.Add(-storeforward.AnswerWait-time.Second), confirmed(models.CallResponseNone))
	text(t, database, sender, recipient, "acked", now.Add(-storeforward.AnswerWait-time.Second), confirmed(models.CallResponseACK))
	text(t, database, sender, recipient, "refused", now.Add(-storeforward.AnswerWait-time.Second), confirmed(models.CallResponseNACK))
	text(t, database, sender, recipient, "still waiting", now.Add(-storeforward.AnswerWait+time.Second), confirmed(models.CallResponseNone))
	text(t, database, sender, recipient, "unconfirmed", now.Add(-storeforward.AnswerWait-time.Second), nil)
	text(t, database, sender, recipient, "long ago", now.Add(-time.Hour), confirmed(models.CallResponseNone))
	s.Sweep(now)
	s.Sweep(now)
	rows := held(t, database)
	require.Len(t, rows, 1)
	assert.Equal(t, noAnswer.ID, rows[0].MessageID)
}

func TestTheSweepEndsTextsHeldTooLong(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{HoldDays: 2})
	now := time.Now()
	old := text(t, database, sender, recipient, "old", now, nil)
	fresh := text(t, database, sender, recipient, "fresh", now, nil)
	require.True(t, s.Hold(old, now.Add(-49*time.Hour), false))
	require.True(t, s.Hold(fresh, now.Add(-47*time.Hour), false))
	s.Sweep(now)
	rows := held(t, database)
	require.Len(t, rows, 2)
	assert.Equal(t, models.HeldStateExpired, rows[0].State)
	assert.NotNil(t, rows[0].DoneAt)
	assert.Equal(t, models.HeldStateHeld, rows[1].State)
}

func TestTurningItOffEndsTheHeldTexts(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{})
	now := time.Now()
	require.True(t, s.Hold(text(t, database, sender, recipient, "x", now, nil), now, false))
	require.NoError(t, models.SetStoreForward(database, recipient, false, now))
	assert.Equal(t, models.HeldStateExpired, held(t, database)[0].State)
}

func TestAHeldTextGoesInTheFormatTheRadioSends(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, f := newService(t, database, config.Messages{})
	now := time.Now()
	radioSent(t, database, now.Add(-time.Hour))
	first := text(t, database, sender, recipient, "first", now.Add(-2*time.Minute), nil)
	second := text(t, database, other, recipient, "second", now.Add(-time.Minute), nil)
	require.True(t, s.Hold(first, now.Add(-2*time.Minute), false))
	require.True(t, s.Hold(second, now.Add(-time.Minute), false))

	s.Heard(recipient, repeater, true)
	s.Flush()

	got := f.all()
	require.Len(t, got, 2)
	for i, want := range []struct {
		src  uint32
		body string
	}{{sender, "first"}, {other, "second"}} {
		assert.Equal(t, uint(repeater), got[i].repeater)
		assert.True(t, got[i].slot)
		body, tx, err := sms.Decode(nil, &got[i].msg.Header, got[i].msg.Payload)
		require.NoError(t, err)
		assert.Equal(t, sms.FormatAnyTone, tx.Format, "the format the radio sends, not the TMS of the sender")
		assert.Equal(t, want.body, string(body))
		assert.Equal(t, want.src, tx.Source)
		assert.Equal(t, uint32(recipient), tx.Destination)
	}
	for _, r := range held(t, database) {
		assert.Equal(t, models.HeldStateDelivered, r.State)
		assert.Equal(t, uint(1), r.Attempts)
	}
}

func TestATextStaysHeldWhileTheFormatOfTheRadioIsNotKnown(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, f := newService(t, database, config.Messages{})
	now := time.Now()
	require.True(t, s.Hold(text(t, database, sender, recipient, "wait", now, nil), now, false))
	text(t, database, recipient, sender, "a text whose format the hub cannot build", now, func(m *models.Message) { m.Rate = 0; m.RepeaterID = repeater })

	s.Heard(recipient, repeater, false)
	s.Flush()

	assert.Empty(t, f.all(), "nothing goes to a radio whose format is not known")
	assert.Equal(t, models.HeldStateHeld, held(t, database)[0].State)
	_, err := storeforward.RadioFormat(database, recipient)
	assert.ErrorIs(t, err, storeforward.ErrNoFormat)
}

func TestATextWithoutAnACKStaysHeldUntilTheLastTry(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, f := newService(t, database, config.Messages{})
	now := time.Now()
	radioSent(t, database, now.Add(-time.Hour))
	require.True(t, s.Hold(text(t, database, sender, recipient, "try", now, nil), now, false))
	f.results = []data.Result{data.ResultRetryLimit, data.ResultRetryLimit, data.ResultRetryLimit, data.ResultRetryLimit, data.ResultRetryLimit}
	for i := 1; i <= 5; i++ {
		s.Heard(recipient, repeater, false)
		s.Flush()
		row := held(t, database)[0]
		assert.Equal(t, uint(i), row.Attempts)
		if i < 5 {
			assert.Equal(t, models.HeldStateHeld, row.State)
		} else {
			assert.Equal(t, models.HeldStateFailed, row.State)
		}
	}
	assert.Len(t, f.all(), 5)
}

func TestAnACKMarksTheTextDeliveredAndANACKFailed(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, f := newService(t, database, config.Messages{})
	now := time.Now()
	radioSent(t, database, now.Add(-time.Hour))
	require.True(t, s.Hold(text(t, database, sender, recipient, "ack me", now.Add(-time.Second), nil), now, false))
	require.True(t, s.Hold(text(t, database, other, recipient, "refuse me", now, nil), now, false))
	f.results = []data.Result{data.ResultDelivered, data.ResultRefused}
	s.Heard(recipient, repeater, false)
	s.Flush()
	rows := held(t, database)
	assert.Equal(t, models.HeldStateDelivered, rows[0].State)
	assert.Equal(t, models.CallResponseACK, rows[0].Response)
	assert.Equal(t, models.HeldStateFailed, rows[1].State)
	assert.Equal(t, models.CallResponseNACK, rows[1].Response)
}

type gatedSender struct {
	*fakeSender
	sending chan struct{}
	gate    chan struct{}
}

func (g *gatedSender) SendData(ctx context.Context, repeater uint, slot bool, m data.Message) (data.Result, error) {
	close(g.sending)
	<-g.gate
	return g.fakeSender.SendData(ctx, repeater, slot, m)
}

// TestTwoReplicasSendAHeldTextOnce lets replica a claim the held text and
// start to send it, then has replica b hear the radio too. Replica b must
// find nothing to send.
func TestTwoReplicasSendAHeldTextOnce(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	a, _ := newService(t, database, config.Messages{})
	b, fb := newService(t, database, config.Messages{})
	ga := &gatedSender{fakeSender: &fakeSender{}, sending: make(chan struct{}), gate: make(chan struct{})}
	a.SetSender(ga)
	now := time.Now()
	radioSent(t, database, now.Add(-time.Hour))
	require.True(t, a.Hold(text(t, database, sender, recipient, "once", now, nil), now, false))

	a.Heard(recipient, repeater, false)
	<-ga.sending
	assert.Equal(t, models.HeldStateSending, held(t, database)[0].State)
	b.Heard(recipient, repeater, false)
	b.Flush()
	close(ga.gate)
	a.Flush()

	assert.Len(t, ga.all(), 1)
	assert.Empty(t, fb.all())
	assert.Equal(t, models.HeldStateDelivered, held(t, database)[0].State)
}

func TestAStreamIsHeldWhicheverOfItsRouteAndItsMessageComesFirst(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	s, _ := newService(t, database, config.Messages{})
	s.Start()
	now := time.Now()
	first := text(t, database, sender, recipient, "route first", now, func(m *models.Message) { m.StreamID = 0xA1 })
	second := text(t, database, sender, recipient, "message first", now, func(m *models.Message) { m.StreamID = 0xA2 })
	routed := text(t, database, sender, recipient, "routed", now, func(m *models.Message) { m.StreamID = 0xA3 })
	s.NotRouted(first.StreamID, recipient)
	s.Stored(first)
	s.Stored(second)
	s.NotRouted(second.StreamID, recipient)
	s.NotRouted(second.StreamID, recipient)
	s.Stored(routed)
	s.NotRouted(routed.StreamID, other)
	s.Flush()
	rows := held(t, database)
	require.Len(t, rows, 2)
	assert.Equal(t, first.ID, rows[0].MessageID)
	assert.Equal(t, second.ID, rows[1].MessageID)
}

// TestOnlyARepeaterOfTheOwnerOrATrustedOneSetsTheFormat sends the text of
// the radio through a hotspot of another user, as a hotspot that sends
// with the ID of the radio could. The hub must not learn the format from
// it, and learns it from a text through the repeater of the owner.
func TestOnlyARepeaterOfTheOwnerOrATrustedOneSetsTheFormat(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	now := time.Now()
	radioSentOn(t, database, now.Add(-time.Hour), otherRepeater)
	_, err := storeforward.RadioFormat(database, recipient)
	require.ErrorIs(t, err, storeforward.ErrNoFormat)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, otherRepeater).Error)
	f, err := storeforward.RadioFormat(database, recipient)
	require.NoError(t, err)
	assert.Equal(t, "AnyTone", f.Name())
}
