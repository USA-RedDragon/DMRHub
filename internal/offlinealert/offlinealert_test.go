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

package offlinealert_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/offlinealert"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	testutils.Main(m)
}

type sentMail struct {
	to, subject, body string
}

func t0() time.Time {
	return time.Unix(1793544060, 0).UTC()
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
	fail bool
}

func (f *fakeMailer) SendText(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("the SMTP server is down")
	}
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

func (f *fakeMailer) take() []sentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

const (
	owner    = uint(3002068)
	cosysop  = uint(3002069)
	noMail   = uint(3002070)
	hotspot  = uint(300206801)
	repeater = uint(990388)
)

type env struct {
	t      *testing.T
	cfg    *config.Config
	db     *gorm.DB
	kv     kv.KV
	mailer *fakeMailer
}

func newEnv(t *testing.T, be testutils.Backend) *env {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	be.Migrated().Setup(t, &cfg)
	cfg.SMTP.Enabled = true
	cfg.NetworkName = "DMRHub Demo"
	cfg.HTTP.CanonicalHost = "https://dmrhub.example.org/"
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = kvStore.Close()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	for _, u := range []models.User{
		{ID: owner, Callsign: "N7CALL", Username: "n7call", Email: "n7call@example.org", EmailVerified: true, Approved: true},
		{ID: cosysop, Callsign: "N6CALL", Username: "kevin", Email: "kevin@example.org", EmailVerified: true, Approved: true},
		{ID: noMail, Callsign: "N0CALL", Username: "other", Email: "other@example.org", Approved: true},
	} {
		require.NoError(t, database.Create(&u).Error)
	}
	for _, id := range []uint{hotspot, repeater} {
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id, Callsign: "N7CALL"}, Type: models.RepeaterTypeMMDVM, Password: "pw", OwnerID: owner}).Error)
	}
	return &env{t: t, cfg: &cfg, db: database, kv: kvStore, mailer: &fakeMailer{}}
}

func (e *env) ping(id uint, at time.Time) {
	e.t.Helper()
	require.NoError(e.t, e.db.Model(&models.Repeater{}).Where("id = ?", id).UpdateColumn("last_ping", at.UTC()).Error)
}

func (e *env) run(at time.Time) offlinealert.Result {
	e.t.Helper()
	require.NoError(e.t, e.kv.Delete(context.Background(), offlinealert.LockKey))
	res, err := offlinealert.NewChecker(e.cfg, e.db, e.kv).WithMailer(e.mailer).Run(context.Background(), at)
	require.NoError(e.t, err)
	return res
}

func forBackends(t *testing.T, f func(t *testing.T, e *env)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, newEnv(t, be))
		})
	}
}

// TestAnOfflineHotspotGetsOneEmailAndABackEmail lets a hotspot stop
// pinging after its owner turned the alert on with a delay of 10 minutes.
// The owner gets one email at 10 minutes, with the last problem of the
// connection log, none more while it stays offline, and one when it is
// back.
func TestAnOfflineHotspotGetsOneEmailAndABackEmail(t *testing.T) {
	t.Parallel()
	forBackends(t, func(t *testing.T, e *env) {
		require.NoError(t, models.SetOfflineAlert(e.db, owner, hotspot, 10, true, t0().Add(-time.Hour)))
		e.ping(hotspot, t0())
		_, err := models.NoteRepeaterEvent(e.db, models.RepeaterEvent{RepeaterID: hotspot, Kind: connlog.Timeout, Detail: "60", LastAt: t0().Add(time.Minute)}, 1)
		require.NoError(t, err)

		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(10*time.Minute-time.Second)))
		assert.Empty(t, e.mailer.take())

		assert.Equal(t, offlinealert.Result{Ran: true, Offline: 1}, e.run(t0().Add(10*time.Minute)))
		sent := e.mailer.take()
		require.Len(t, sent, 1)
		assert.Equal(t, "n7call@example.org", sent[0].to)
		assert.Equal(t, "Hotspot N7CALL 300206801 is offline", sent[0].subject)
		assert.Equal(t, "Your hotspot N7CALL 300206801 has been offline on DMRHub Demo since Nov 1, 14:41 UTC (10 minutes).\n\n"+
			"Last problem: Connection lost: no ping for 60 seconds. Check the internet connection of the hotspot.\n\n"+
			"Open the connection log: https://dmrhub.example.org/repeaters/300206801\n\n"+
			"To stop these emails, turn off \"Email me when it is offline\" on the same page.\n", sent[0].body)

		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(time.Hour)))
		assert.Empty(t, e.mailer.take())

		e.ping(hotspot, t0().Add(70*time.Minute))
		assert.Equal(t, offlinealert.Result{Ran: true, Back: 1}, e.run(t0().Add(71*time.Minute)))
		sent = e.mailer.take()
		require.Len(t, sent, 1)
		assert.Equal(t, "Hotspot N7CALL 300206801 is back", sent[0].subject)
		assert.Contains(t, sent[0].body, "It was offline for 70 minutes.")
		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(72*time.Minute)))
		assert.Empty(t, e.mailer.take())

		assert.Equal(t, offlinealert.Result{Ran: true, Offline: 1}, e.run(t0().Add(80*time.Minute)), "the next outage gets its own email")
	})
}

// TestAMovedOrDrainedHotspotGetsNoEmail keeps a hotspot pinging through
// another replica, as after a move or a drain of its replica, while the
// first replica's view of it is gone. Its pings reach the database from
// whichever replica serves it, so no email goes out.
func TestAMovedOrDrainedHotspotGetsNoEmail(t *testing.T) {
	t.Parallel()
	forBackends(t, func(t *testing.T, e *env) {
		require.NoError(t, models.SetOfflineAlert(e.db, owner, hotspot, 5, true, t0().Add(-time.Hour)))
		for m := range 30 {
			at := t0().Add(time.Duration(m) * time.Minute)
			e.ping(hotspot, at.Add(-30*time.Second))
			assert.Equal(t, offlinealert.Result{Ran: true}, e.run(at))
		}
		assert.Empty(t, e.mailer.take())
	})
}

// TestTwoReplicasSendOneEmail runs the check of two replicas on one
// outage, as when the lock passes from one to the other: the second finds
// the email sent.
func TestTwoReplicasSendOneEmail(t *testing.T) {
	t.Parallel()
	forBackends(t, func(t *testing.T, e *env) {
		require.NoError(t, models.SetOfflineAlert(e.db, owner, hotspot, 5, false, t0().Add(-time.Hour)))
		e.ping(hotspot, t0())
		other := &fakeMailer{}
		first := offlinealert.NewChecker(e.cfg, e.db, e.kv).WithMailer(e.mailer)
		second := offlinealert.NewChecker(e.cfg, e.db, e.kv).WithMailer(other)
		res, err := first.Run(context.Background(), t0().Add(6*time.Minute))
		require.NoError(t, err)
		assert.Equal(t, 1, res.Offline)
		res, err = second.Run(context.Background(), t0().Add(6*time.Minute))
		require.NoError(t, err)
		assert.False(t, res.Ran, "the other replica holds the lock")
		require.NoError(t, e.kv.Delete(context.Background(), offlinealert.LockKey))
		res, err = second.Run(context.Background(), t0().Add(7*time.Minute))
		require.NoError(t, err)
		assert.Zero(t, res.Offline)
		assert.Len(t, e.mailer.take(), 1)
		assert.Empty(t, other.take())

		e.ping(hotspot, t0().Add(8*time.Minute))
		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(9*time.Minute)), "no back email when it was not asked for")
		assert.Empty(t, e.mailer.take())
	})
}

// TestNoEmailWithoutAReasonToSend covers the alerts that send nothing: one
// turned on while the hotspot was offline, one of a user without a
// verified address, one of a co-sysop who was removed, and every alert
// while SMTP is off. A failed email goes out at the next check.
func TestNoEmailWithoutAReasonToSend(t *testing.T) {
	t.Parallel()
	forBackends(t, func(t *testing.T, e *env) {
		e.ping(hotspot, t0())
		e.ping(repeater, t0())
		require.NoError(t, models.SetOfflineAlert(e.db, owner, hotspot, 5, false, t0().Add(time.Minute)))
		require.NoError(t, models.SetOfflineAlert(e.db, noMail, repeater, 5, false, t0().Add(-time.Hour)))
		require.NoError(t, models.SetOfflineAlert(e.db, cosysop, repeater, 5, false, t0().Add(-time.Hour)))
		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(time.Hour)))
		assert.Empty(t, e.mailer.take())

		_, err := models.InviteCosysop(e.db, repeater, cosysop, owner, t0())
		require.NoError(t, err)
		_, err = models.AnswerCosysopInvite(e.db, repeater, cosysop, true, t0())
		require.NoError(t, err)
		require.NoError(t, models.SetOfflineAlert(e.db, cosysop, repeater, 5, false, t0().Add(-time.Hour)))
		e.cfg.SMTP.Enabled = false
		assert.Equal(t, offlinealert.Result{}, e.run(t0().Add(time.Hour)))
		e.cfg.SMTP.Enabled = true
		e.mailer.fail = true
		assert.Equal(t, offlinealert.Result{Ran: true}, e.run(t0().Add(time.Hour)))
		e.mailer.fail = false
		assert.Equal(t, offlinealert.Result{Ran: true, Offline: 1}, e.run(t0().Add(61*time.Minute)))
		sent := e.mailer.take()
		require.Len(t, sent, 1)
		assert.Equal(t, "kevin@example.org", sent[0].to)
		assert.Equal(t, "Repeater N7CALL 990388 is offline", sent[0].subject)

		_, err = models.RemoveCosysop(e.db, repeater, cosysop)
		require.NoError(t, err)
		_, found, err := models.FindOfflineAlert(e.db, cosysop, repeater)
		require.NoError(t, err)
		assert.False(t, found, "the alert ends with the co-sysop role")
	})
}
