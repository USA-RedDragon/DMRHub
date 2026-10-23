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

package messages_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	owner   = uint(1000001)
	other   = uint(1000002)
	addedID = uint(1000011)
	tg      = uint(3100)
)

type env struct {
	t      *testing.T
	router *gin.Engine
	db     *gorm.DB
	jar    testutils.CookieJar
	ps     pubsub.PubSub
}

func newEnv(t *testing.T) *env {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()
	testutils.SeedAccount(t, db, owner, "KI5VMF", "owner")
	testutils.SeedAccount(t, db, other, "KD5SMP", "other")
	require.NoError(t, db.Create(&models.Talkgroup{ID: tg, Name: "Texas"}).Error)
	return &env{t: t, router: router, db: db, jar: testutils.Login(t, router, "owner"), ps: tdb.PubSub()}
}

func (e *env) storage(on bool) time.Time {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, e.jar, http.MethodPut, "/api/v1/users/me/settings/sms-storage", map[string]bool{"enabled": on})
	require.Equal(e.t, http.StatusOK, w.Code)
	var u models.User
	require.NoError(e.t, e.db.First(&u, owner).Error)
	if u.SMSStorageSince == nil {
		return time.Time{}
	}
	return *u.SMSStorageSince
}

func (e *env) msg(at time.Time, src, dst uint, group bool, text string) models.Message {
	e.t.Helper()
	m := models.Message{ReceivedAt: at, SourceID: src, DestinationID: dst, IsGroup: group, Format: models.MessageFormatIP, Text: &text, ChecksumOK: true}
	require.NoError(e.t, e.db.Create(&m).Error)
	return m
}

func (e *env) list(path string) apimodels.MessageList {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, e.jar, http.MethodGet, path, nil)
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	var l apimodels.MessageList
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &l))
	return l
}

func (e *env) stats() apimodels.MessageStats {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, e.jar, http.MethodGet, "/api/v1/messages/stats", nil)
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	var s apimodels.MessageStats
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func (e *env) get(id uint) int {
	e.t.Helper()
	return testutils.Do(e.t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/messages/%d", id), nil).Code
}

func texts(l apimodels.MessageList) []string {
	out := make([]string, 0, len(l.Messages))
	for _, m := range l.Messages {
		out = append(out, *m.Text)
	}
	return out
}

func TestMessagesNeedLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, p := range []string{"/api/v1/messages/inbox", "/api/v1/messages/outbox", "/api/v1/messages/stats", "/api/v1/messages/1"} {
		w := testutils.Do(t, e.router, testutils.CookieJar{}, http.MethodGet, p, nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, p)
	}
}

func TestStorageOffShowsNothingButTheHubKeepsEverything(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m1 := e.msg(time.Now(), other, owner, false, "to owner")
	m2 := e.msg(time.Now(), owner, other, false, "from owner")

	for _, p := range []string{"/api/v1/messages/inbox", "/api/v1/messages/outbox"} {
		l := e.list(p)
		assert.Equal(t, 0, l.Total, p)
		assert.Empty(t, l.Messages, p)
	}
	assert.Equal(t, http.StatusNotFound, e.get(m1.ID))
	assert.Equal(t, http.StatusNotFound, e.get(m2.ID))
	s := e.stats()
	assert.Zero(t, s.Inbox+s.Outbox+s.ACK+s.NACK)
	assert.Empty(t, s.ByFormat)
	var n int64
	require.NoError(t, e.db.Model(&models.Message{}).Count(&n).Error)
	assert.Equal(t, int64(2), n)

	since := e.storage(true)
	e.storage(false)
	assert.True(t, since.After(m2.ReceivedAt))
	assert.Equal(t, 0, e.list("/api/v1/messages/inbox").Total)
}

func TestOnlyMessagesAfterTheOptInShow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	before := e.msg(time.Now().Add(-time.Hour), other, owner, false, "before")
	since := e.storage(true)
	after := e.msg(since.Add(time.Millisecond), other, owner, false, "after")

	l := e.list("/api/v1/messages/inbox")
	assert.Equal(t, 1, l.Total)
	assert.Equal(t, []string{"after"}, texts(l))
	assert.Equal(t, http.StatusNotFound, e.get(before.ID))
	assert.Equal(t, http.StatusOK, e.get(after.ID))
	assert.Equal(t, 1, e.stats().Inbox)
}

func TestOffAndOnHidesTheMessagesInBetween(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first := e.storage(true)
	between := e.msg(first.Add(time.Millisecond), other, owner, false, "between")
	assert.Equal(t, 1, e.list("/api/v1/messages/inbox").Total)

	time.Sleep(10 * time.Millisecond)
	e.storage(false)
	second := e.storage(true)
	require.True(t, second.After(between.ReceivedAt))
	var u models.User
	require.NoError(t, e.db.First(&u, owner).Error)
	require.NotNil(t, u.SMSStorageSince)
	assert.True(t, u.SMSStorageSince.Equal(second), "the time is the latest enable")

	later := e.msg(second.Add(time.Millisecond), other, owner, false, "later")
	l := e.list("/api/v1/messages/inbox")
	assert.Equal(t, []string{"later"}, texts(l))
	assert.Equal(t, http.StatusNotFound, e.get(between.ID))
	assert.Equal(t, http.StatusOK, e.get(later.ID))
}

func TestOnlyOwnedRadioIDsShow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	at := since.Add(time.Millisecond)
	notMine := e.msg(at, other, uint(1000099), false, "not mine")
	e.msg(at, uint(1000099), other, false, "not mine either")
	toAdded := e.msg(at.Add(time.Millisecond), other, addedID, false, "to added")
	assert.Equal(t, 0, e.list("/api/v1/messages/inbox").Total)

	require.NoError(t, e.db.Create(&models.UserRadioID{UserID: owner, RadioID: addedID, VerifiedAt: since.Add(-time.Hour)}).Error)
	l := e.list("/api/v1/messages/inbox")
	assert.Equal(t, []string{"to added"}, texts(l))
	assert.Equal(t, http.StatusOK, e.get(toAdded.ID))
	assert.Equal(t, http.StatusNotFound, e.get(notMine.ID))
	assert.Equal(t, 0, e.list("/api/v1/messages/outbox").Total)

	require.NoError(t, e.db.Where("radio_id = ?", addedID).Delete(&models.UserRadioID{}).Error)
	assert.Equal(t, 0, e.list("/api/v1/messages/inbox").Total)
	assert.Equal(t, http.StatusNotFound, e.get(toAdded.ID))
}

func TestAnIDAddedAfterTheOptInShowsNothingFromBeforeItWasAdded(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	early := e.msg(since.Add(time.Millisecond), other, addedID, false, "early")
	verified := since.Add(time.Second)
	require.NoError(t, e.db.Create(&models.UserRadioID{UserID: owner, RadioID: addedID, VerifiedAt: verified}).Error)
	late := e.msg(verified.Add(time.Millisecond), other, addedID, false, "late")

	assert.Equal(t, []string{"late"}, texts(e.list("/api/v1/messages/inbox")))
	assert.Equal(t, http.StatusNotFound, e.get(early.ID))
	assert.Equal(t, http.StatusOK, e.get(late.ID))
}

func TestGroupMessagesAreInTheOutboxOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	mine := e.msg(since.Add(time.Millisecond), owner, tg, true, "CQ Texas")
	e.msg(since.Add(2*time.Millisecond), other, tg, true, "theirs")

	assert.Equal(t, 0, e.list("/api/v1/messages/inbox").Total)
	l := e.list("/api/v1/messages/outbox")
	require.Len(t, l.Messages, 1)
	m := l.Messages[0]
	assert.Equal(t, mine.ID, m.ID)
	assert.True(t, m.Group)
	assert.Equal(t, "Texas", m.TalkgroupName)
	assert.Equal(t, "KI5VMF", m.SourceCallsign)
	assert.Empty(t, m.DestinationCallsign)
	assert.Equal(t, "none", m.Response)
}

func TestTheCapsHold(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	base := since.Add(time.Second)
	rows := make([]models.Message, 0, 600)
	for i := range 300 {
		in, out := fmt.Sprintf("in %03d", i), fmt.Sprintf("out %03d", i)
		at := base.Add(time.Duration(i) * time.Second)
		rows = append(rows,
			models.Message{ReceivedAt: at, SourceID: other, DestinationID: owner, Format: models.MessageFormatIP, Text: &in, Response: models.CallResponseACK, Confirmed: true},
			models.Message{ReceivedAt: at, SourceID: owner, DestinationID: other, Format: models.MessageFormatTMS, Text: &out},
		)
	}
	require.NoError(t, e.db.CreateInBatches(rows, 100).Error)

	for _, b := range []string{"inbox", "outbox"} {
		l := e.list("/api/v1/messages/" + b + "?limit=50")
		assert.Equal(t, 250, l.Total, b)
		assert.Len(t, l.Messages, 50, b)
		last := e.list("/api/v1/messages/" + b + "?limit=50&page=5")
		require.Len(t, last.Messages, 50, b)
		assert.Equal(t, fmt.Sprintf("%s 050", b[:len(b)-3]), *last.Messages[49].Text, b)
		assert.Empty(t, e.list("/api/v1/messages/"+b+"?limit=50&page=6").Messages, b)
		assert.Empty(t, e.list("/api/v1/messages/"+b+"?limit=10&page=26").Messages, b)
		assert.Empty(t, e.list("/api/v1/messages/"+b+"?limit=50&page=99999999999").Messages, b)
		assert.Len(t, e.list("/api/v1/messages/"+b+"?limit=500").Messages, 50, b)
	}

	var row251, row250 models.Message
	require.NoError(t, e.db.Where("text = ?", "in 049").First(&row251).Error)
	require.NoError(t, e.db.Where("text = ?", "in 050").First(&row250).Error)
	assert.Equal(t, http.StatusNotFound, e.get(row251.ID))
	assert.Equal(t, http.StatusOK, e.get(row250.ID))

	s := e.stats()
	assert.Equal(t, 250, s.Inbox)
	assert.Equal(t, 250, s.Outbox)
	assert.Equal(t, 250, s.ACK)
	assert.Equal(t, map[string]int{"ip": 250, "tms": 250}, s.ByFormat)

	e.msg(base.Add(time.Hour), other, owner, false, "newest")
	assert.Equal(t, 250, e.list("/api/v1/messages/inbox").Total)
	assert.Equal(t, http.StatusNotFound, e.get(row250.ID), "a new message pushes the oldest out")
	assert.Equal(t, 250, e.list("/api/v1/messages/outbox").Total)
	assert.Equal(t, 250, e.stats().Outbox)
}

func TestStatsCountEachMessageOnceAndByDay(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	self := e.msg(since.Add(time.Millisecond), owner, owner, false, "note to self")
	require.NoError(t, e.db.Model(&self).Update("response", models.CallResponseNACK).Error)
	s := e.stats()
	assert.Equal(t, 1, s.Inbox)
	assert.Equal(t, 1, s.Outbox)
	assert.Equal(t, 1, s.NACK)
	assert.Equal(t, map[string]int{"ip": 1}, s.ByFormat)
	require.Len(t, s.ByDay, 7)
	assert.Equal(t, time.Now().UTC().Format("2006-01-02"), s.ByDay[6].Day)
	assert.Equal(t, 1, s.ByDay[6].Count)

	w := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/messages/stats?tz=America/Chicago", nil)
	require.Equal(t, http.StatusOK, w.Code)
	w = testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/messages/stats?tz=Not/AZone", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMessageResponsesHaveNoOptInTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	m := e.msg(since.Add(2*time.Second), other, owner, false, "hi")
	for _, p := range []string{"/api/v1/messages/inbox", "/api/v1/messages/outbox", "/api/v1/messages/stats", fmt.Sprintf("/api/v1/messages/%d", m.ID)} {
		w := testutils.Do(t, e.router, e.jar, http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, w.Code, p)
		keys := testutils.JSONKeys(t, w.Body.Bytes())
		for _, k := range []string{"email", "sms_storage_since", "sms_storage_enabled", "since", "raw", "repeater_id", "transport"} {
			assert.NotContains(t, keys, k, p)
		}
		assert.NotContains(t, w.Body.String(), since.Format("2006-01-02T15:04:05"), p)
	}
}

func TestUserViewShowsTheSizeOfDataThatIsNotText(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	since := e.storage(true)
	m := models.Message{ReceivedAt: since.Add(time.Millisecond), SourceID: other, DestinationID: owner, Format: models.MessageFormatData, Octets: 92, Raw: make([]byte, 104)}
	require.NoError(t, e.db.Create(&m).Error)
	l := e.list("/api/v1/messages/inbox")
	require.Len(t, l.Messages, 1)
	assert.Nil(t, l.Messages[0].Text)
	assert.Equal(t, "data", l.Messages[0].Format)
	assert.Equal(t, uint(92), l.Messages[0].Octets)
	w := testutils.Do(t, e.router, e.jar, http.MethodGet, fmt.Sprintf("/api/v1/messages/%d", m.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	keys := testutils.JSONKeys(t, w.Body.Bytes())
	assert.True(t, keys["octets"])
	assert.False(t, keys["raw"])
}
