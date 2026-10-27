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

package radioiddrift_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/radioiddrift"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setup(t *testing.T, be testutils.Backend) (*config.Config, *gorm.DB, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_drift_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	be.Migrated().Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = kvStore.Close()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	return &cfg, database, kvStore
}

type directory map[uint]string

func (d directory) lookup(id uint) (string, bool) {
	c, ok := d[id]
	return c, ok
}

func (d directory) size() int {
	return len(d)
}

func drifts(t *testing.T, database *gorm.DB) map[uint]models.RadioIDDrift {
	t.Helper()
	var rows []models.RadioIDDrift
	require.NoError(t, database.Find(&rows).Error)
	out := make(map[uint]models.RadioIDDrift, len(rows))
	for _, r := range rows {
		out[r.RadioID] = r
	}
	return out
}

func TestCheckFindsRadioIDsThatRadioIDNoLongerMatches(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			cfg.DMR.DisableRadioIDValidation = false
			for _, u := range []models.User{
				{ID: 3100001, Callsign: "N0SAME", Username: "same"},
				{ID: 3100002, Callsign: "N0OLD", Username: "moved"},
				{ID: 3100003, Callsign: "N0GONE", Username: "gone"},
				{ID: 3100004, Callsign: "N0ADMIN", Username: "chosen"},
				{ID: 3100005, Callsign: "N0DEL", Username: "deleted"},
			} {
				require.NoError(t, database.Create(&u).Error)
			}
			for _, r := range []models.UserRadioID{
				{UserID: 3100001, RadioID: 3100011},
				{UserID: 3100001, RadioID: 3100012},
				{UserID: 3100001, RadioID: 3100013},
				{UserID: 3100004, RadioID: 3100041},
			} {
				r.VerifiedAt = time.Now()
				require.NoError(t, database.Create(&r).Error)
			}
			require.NoError(t, models.SetCallsignOverride(database, 3100004, true))
			require.NoError(t, database.Delete(&models.User{ID: 3100005}).Error)
			dir := directory{3100001: "n0same", 3100011: "N0SAME", 3100012: "N0OTHER", 3100002: "N0NEW", 3100004: "N0RID", 3100041: "N0RID", 3100005: "N0RID2", 9999999: "N0NONE"}

			first := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
			res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), first)
			require.NoError(t, err)
			assert.Equal(t, radioiddrift.Result{Ran: true, Checked: 6, Drifts: 4}, res)
			got := drifts(t, database)
			require.Len(t, got, 4)
			assert.Equal(t, models.RadioIDDrift{RadioID: 3100002, UserID: 3100002, RadioIDCallsign: "N0NEW"}, bare(got[3100002]))
			assert.Equal(t, models.RadioIDDrift{RadioID: 3100003, UserID: 3100003}, bare(got[3100003]))
			assert.Equal(t, models.RadioIDDrift{RadioID: 3100012, UserID: 3100001, RadioIDCallsign: "N0OTHER"}, bare(got[3100012]))
			assert.Equal(t, models.RadioIDDrift{RadioID: 3100013, UserID: 3100001}, bare(got[3100013]))
			assert.WithinDuration(t, first, got[3100003].FoundAt, time.Second)

			rows, err := models.ListRadioIDDrifts(database)
			require.NoError(t, err)
			require.Len(t, rows, 4)
			assert.Equal(t, uint(3100002), rows[0].RadioID)
			assert.Equal(t, "moved", rows[0].Username)
			assert.Equal(t, "N0OLD", rows[0].Callsign)
			n, err := models.CountRadioIDDrifts(database)
			require.NoError(t, err)
			assert.Equal(t, int64(4), n)

			dir[3100002] = "N0OLD"
			second := first.Add(24 * time.Hour)
			require.NoError(t, kvStore.Delete(context.Background(), radioiddrift.LockKey))
			res, err = radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), second)
			require.NoError(t, err)
			assert.Equal(t, radioiddrift.Result{Ran: true, Checked: 6, Drifts: 3}, res)
			got = drifts(t, database)
			require.Len(t, got, 3)
			assert.WithinDuration(t, first, got[3100003].FoundAt, time.Second)
			assert.WithinDuration(t, second, got[3100003].CheckedAt, time.Second)
		})
	}
}

func bare(d models.RadioIDDrift) models.RadioIDDrift {
	d.FoundAt, d.CheckedAt, d.NotifiedAt = time.Time{}, time.Time{}, nil
	return d
}

type sentMail struct {
	to, subject, body string
}

type fakeMailer struct {
	sent []sentMail
}

func (f *fakeMailer) SendText(to, subject, body string) error {
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

func seedVerified(t *testing.T, database *gorm.DB, id uint, callsign string) {
	t.Helper()
	u := models.User{ID: id, Callsign: callsign, Username: fmt.Sprintf("u%d", id), Email: fmt.Sprintf("u%d@example.com", id), EmailVerified: true}
	require.NoError(t, database.Create(&u).Error)
}

func TestCheckEmailsAUserOnceForEachFinding(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, database, kvStore := setup(t, be)
			cfg.DMR.DisableRadioIDValidation = false
			cfg.SMTP.Enabled = true
			cfg.NetworkName = "Test Hub"
			dir := directory{}
			for i := uint(0); i < 40; i++ {
				seedVerified(t, database, 3100100+i, fmt.Sprintf("N%dAA", i))
				dir[3100100+i] = fmt.Sprintf("N%dAA", i)
			}
			require.NoError(t, database.Create(&models.User{ID: 3100200, Callsign: "N0MAIL", Username: "nomail"}).Error)
			dir[3100100] = "K5NEW"
			dir[3100200] = "K5NEW2"
			mailer := &fakeMailer{}
			check := func(at time.Time) radioiddrift.Result {
				t.Helper()
				require.NoError(t, kvStore.Delete(context.Background(), radioiddrift.LockKey))
				res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).WithMailer(mailer).Run(context.Background(), at)
				require.NoError(t, err)
				return res
			}
			now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)

			res := check(now)
			assert.Equal(t, radioiddrift.Result{Ran: true, Checked: 41, Drifts: 2, Notified: 1}, res)
			require.Len(t, mailer.sent, 1)
			assert.Equal(t, "u3100100@example.com", mailer.sent[0].to)
			assert.Equal(t, "Your callsign on Test Hub does not match RadioID", mailer.sent[0].subject)
			assert.Contains(t, mailer.sent[0].body, "RadioID lists DMR ID 3100100 under K5NEW")
			assert.Contains(t, mailer.sent[0].body, "N0AA")
			d, ok, err := models.FindRadioIDDrift(database, 3100100)
			require.NoError(t, err)
			require.True(t, ok)
			require.NotNil(t, d.NotifiedAt)

			res = check(now.Add(24 * time.Hour))
			assert.Equal(t, 0, res.Notified, "the same finding is not mailed again")
			assert.Len(t, mailer.sent, 1)

			dir[3100101] = "K5NEW3"
			res = check(now.Add(48 * time.Hour))
			assert.Equal(t, 1, res.Notified)
			require.Len(t, mailer.sent, 2)
			assert.Equal(t, "u3100101@example.com", mailer.sent[1].to)
		})
	}
}

func TestCheckMailsNobodyWhenTooManyDrift(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	cfg.SMTP.Enabled = true
	dir := directory{}
	for i := uint(0); i < 100; i++ {
		seedVerified(t, database, 3100100+i, fmt.Sprintf("N%dAA", i))
		dir[3100100+i] = fmt.Sprintf("N%dAA", i)
	}
	for i := uint(0); i < 6; i++ {
		dir[3100100+i] = "BAD"
	}
	mailer := &fakeMailer{}
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).WithMailer(mailer).Run(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, radioiddrift.Result{Ran: true, Checked: 100, Drifts: 6}, res)
	assert.Empty(t, mailer.sent)
	d, ok, err := models.FindRadioIDDrift(database, 3100100)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Nil(t, d.NotifiedAt, "a skipped email can go out on a later night")
}

func TestCheckMailsAFewDriftsOfASmallHub(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	cfg.SMTP.Enabled = true
	dir := directory{}
	for i := uint(0); i < 10; i++ {
		seedVerified(t, database, 3100100+i, fmt.Sprintf("N%dAA", i))
		dir[3100100+i] = fmt.Sprintf("N%dAA", i)
	}
	for i := uint(0); i < 5; i++ {
		dir[3100100+i] = "MOVED"
	}
	mailer := &fakeMailer{}
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).WithMailer(mailer).Run(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, radioiddrift.Result{Ran: true, Checked: 10, Drifts: 5, Notified: 5}, res)
	assert.Len(t, mailer.sent, 5)
}

func TestCheckMailsNobodyWithoutSMTP(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	cfg.SMTP.Enabled = false
	seedVerified(t, database, 3100100, "N0AA")
	dir := directory{3100100: "K5NEW"}
	mailer := &fakeMailer{}
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).WithMailer(mailer).Run(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, 0, res.Notified)
	assert.Empty(t, mailer.sent)
}

func TestCheckRunsOnOneReplica(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	dir := directory{3100002: "N0NEW"}
	now := time.Now()
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.NoError(t, err)
	assert.True(t, res.Ran)
	res, err = radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, res.Ran)
}

func TestCheckClearsTheListWithoutRadioIDValidation(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = true
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	now := time.Now()
	require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{RadioID: 3100002, UserID: 3100002, RadioIDCallsign: "N0NEW"}}, now))
	dir := directory{3100002: "N0NEW"}
	res, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.NoError(t, err)
	assert.False(t, res.Ran)
	assert.Empty(t, drifts(t, database))
}

func TestCheckRefusesAnEmptyRadioIDDatabase(t *testing.T) {
	t.Parallel()
	cfg, database, kvStore := setup(t, testutils.SQLiteMemoryBackend())
	cfg.DMR.DisableRadioIDValidation = false
	require.NoError(t, database.Create(&models.User{ID: 3100002, Callsign: "N0OLD", Username: "moved"}).Error)
	now := time.Now()
	require.NoError(t, models.ReplaceRadioIDDrifts(database, []models.RadioIDDrift{{RadioID: 3100002, UserID: 3100002, RadioIDCallsign: "N0NEW"}}, now))
	dir := directory{}
	_, err := radioiddrift.NewCheckerWith(cfg, database, kvStore, dir.lookup, dir.size).Run(context.Background(), now)
	require.Error(t, err)
	assert.Len(t, drifts(t, database), 1)
}

func TestNextRunIsTheNextOneOClock(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("CDT", -5*3600)
	for from, want := range map[time.Time]time.Time{
		time.Date(2026, 10, 27, 0, 30, 0, 0, loc):  time.Date(2026, 10, 27, 1, 0, 0, 0, loc),
		time.Date(2026, 10, 27, 1, 0, 0, 0, loc):   time.Date(2026, 10, 28, 1, 0, 0, 0, loc),
		time.Date(2026, 10, 27, 23, 59, 0, 0, loc): time.Date(2026, 10, 28, 1, 0, 0, 0, loc),
	} {
		assert.Equal(t, want, radioiddrift.NextRun(from), from.String())
	}
}
