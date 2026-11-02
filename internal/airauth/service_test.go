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

package airauth_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	owner    = uint(1000001)
	other    = uint(1000002)
	radio    = uint(1000101)
	repeater = uint(100101)
	hotspot  = uint(100102)
)

type env struct {
	db    *gorm.DB
	store kv.KV
	clock *clock.Fake
	key   []byte
	svc   *airauth.Service
	len   time.Duration
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(memdb.DSN()))
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.UserRadioID{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	fake := clock.NewFake(time.Date(2026, 11, 1, 12, 0, 10, 0, time.UTC))
	store, err := kv.MakeKV(context.Background(), &config.Config{}, kv.WithClock(fake))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for _, u := range []models.User{{ID: owner, Callsign: "N0CODE", Username: "n0code"}, {ID: other, Callsign: "N1CODE", Username: "n1code"}} {
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Create(&models.UserRadioID{UserID: owner, RadioID: radio, VerifiedAt: fake.Now()}).Error)
	e := &env{db: db, store: store, clock: fake, len: 24 * time.Hour,
		key: airauth.KeyOf(&config.Config{Secret: "a-secret-of-fifteen", PasswordSalt: "a-salt-of-fifteen!"})}
	e.svc = e.service()
	return e
}

func (e *env) service() *airauth.Service {
	return airauth.New(e.db, e.store, e.clock, func() []byte { return e.key }, func() time.Duration { return e.len })
}

func (e *env) enroll(t *testing.T) []byte {
	t.Helper()
	setup, err := e.svc.Begin(context.Background(), owner, "DMRHub", "n0code")
	require.NoError(t, err)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	require.NoError(t, err)
	require.NoError(t, e.svc.Confirm(context.Background(), owner, e.code(secret, 0)))
	e.clock.Advance(airauth.Period)
	return secret
}

func (e *env) code(secret []byte, steps int64) uint32 {
	return airauth.Code(secret, airauth.Step(e.clock.Now())+steps)
}

func TestSetupTurnsOnOnlyWithARightCodeAndKeepsTheSecretSealed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	setup, err := e.svc.Begin(ctx, owner, "DMRHub", "n0code")
	require.NoError(t, err)
	assert.Contains(t, setup.URI, setup.Key)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	require.NoError(t, err)

	on, err := e.svc.Enabled(owner)
	require.NoError(t, err)
	assert.False(t, on, "Begin must not turn the on-air code on")
	require.ErrorIs(t, e.svc.Confirm(ctx, owner, e.code(secret, 2)), airauth.ErrWrongCode)
	require.NoError(t, e.svc.Confirm(ctx, owner, e.code(secret, 0)))
	on, err = e.svc.Enabled(owner)
	require.NoError(t, err)
	assert.True(t, on)

	sealed, err := models.TOTPSecretOf(e.db, owner)
	require.NoError(t, err)
	assert.NotContains(t, sealed, setup.Key)
	got, err := airauth.Open(e.key, owner, sealed)
	require.NoError(t, err)
	assert.Equal(t, secret, got)

	_, err = e.svc.Begin(ctx, owner, "DMRHub", "n0code")
	require.ErrorIs(t, err, airauth.ErrOn, "a second setup must not replace the secret without turning it off")
	require.ErrorIs(t, e.svc.Confirm(ctx, owner, e.code(secret, 0)), airauth.ErrNoSetup)
}

func TestASetupEndsAfterItsTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	setup, err := e.svc.Begin(context.Background(), owner, "DMRHub", "n0code")
	require.NoError(t, err)
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	e.clock.Advance(airauth.SetupTTL + time.Second)
	require.ErrorIs(t, e.svc.Confirm(context.Background(), owner, e.code(secret, 0)), airauth.ErrNoSetup)
}

func TestARightCodeUnlocksTheRadioOnlyOnItsRepeaterSlot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)

	got := e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: repeater, Slot2: true})
	require.Equal(t, airauth.Granted, got)
	grants, err := e.svc.Grants(ctx, radio)
	require.NoError(t, err)
	now := e.clock.Now()
	assert.True(t, airauth.Unlocked(grants, repeater, true, now))
	assert.False(t, airauth.Unlocked(grants, repeater, false, now), "the other slot stays locked")
	assert.False(t, airauth.Unlocked(grants, hotspot, true, now), "another repeater stays locked")
	other, err := e.svc.Grants(ctx, owner)
	require.NoError(t, err)
	assert.Empty(t, other, "another radio ID of the user stays locked")

	e.clock.Advance(e.len - time.Second)
	grants, err = e.svc.Grants(ctx, radio)
	require.NoError(t, err)
	assert.True(t, airauth.Unlocked(grants, repeater, true, e.clock.Now()))
	e.clock.Advance(time.Second)
	grants, err = e.svc.Grants(ctx, radio)
	require.NoError(t, err)
	assert.False(t, airauth.Unlocked(grants, repeater, true, e.clock.Now()), "the unlock ends after its length")
}

func TestEachTimeStepWorksOncePerUserOnEveryReplica(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	replica2 := e.service()

	code := e.code(secret, 0)
	require.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: code, Repeater: repeater}))
	assert.Equal(t, airauth.Refused, replica2.Attempt(ctx, airauth.Try{Src: owner, Owner: owner, Code: code, Repeater: hotspot}),
		"a code heard on the air must not work again on another replica")
	assert.Equal(t, airauth.Refused, replica2.Attempt(ctx, airauth.Try{Src: owner, Owner: owner, Code: e.code(secret, -1), Repeater: hotspot}),
		"the code of an earlier step must not work after a later one")
	assert.Equal(t, airauth.Granted, replica2.Attempt(ctx, airauth.Try{Src: owner, Owner: owner, Code: e.code(secret, 1), Repeater: hotspot}),
		"the code of the next step works once")
}

func TestACodeWorksOnlyForItsOwner(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	code := e.code(secret, 0)
	assert.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: other, Owner: other, Code: code, Repeater: repeater}),
		"a user without an on-air code cannot unlock with the code of another")
	assert.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: 3100001, Code: code, Repeater: repeater}),
		"a radio ID without an owner cannot unlock")
	assert.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: code + 1, Repeater: repeater}))
	assert.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: code, Repeater: repeater}))
}

func TestTriesAreLimitedPerRadioAndPerRepeater(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	for i := range airauth.SourceTries {
		require.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: 1000000 - 1 - uint32(i), Repeater: repeater}))
	}
	assert.Equal(t, airauth.Limited, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: hotspot}),
		"a radio ID that tried too often must wait, even with the right code")

	for i := range airauth.IngressTries - airauth.SourceTries {
		require.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: 3100000 + uint(i), Code: 1, Repeater: repeater}))
	}
	assert.Equal(t, airauth.Limited, e.svc.Attempt(ctx, airauth.Try{Src: owner, Owner: owner, Code: e.code(secret, 0), Repeater: repeater}),
		"a repeater that carried too many tries must wait")

	e.clock.Advance(5 * time.Minute)
	assert.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: repeater}))
}

func TestWebCodesAreLimitedPerUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	for range airauth.UserTries - 1 {
		_, err := e.svc.Disable(ctx, owner, 1000000-1)
		require.ErrorIs(t, err, airauth.ErrWrongCode)
	}
	_, err := e.svc.Disable(ctx, owner, e.code(secret, 0))
	require.ErrorIs(t, err, airauth.ErrLimited)
	on, err := e.svc.Enabled(owner)
	require.NoError(t, err)
	assert.True(t, on)
}

func TestTurningOffNeedsACodeAndEndsTheUnlocks(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	require.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: repeater}))
	e.clock.Advance(airauth.Period)

	_, err := e.svc.Disable(ctx, owner, e.code(secret, 5))
	require.ErrorIs(t, err, airauth.ErrWrongCode)
	radios, err := e.svc.Disable(ctx, owner, e.code(secret, 0))
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint{owner, radio}, radios)
	on, err := e.svc.Enabled(owner)
	require.NoError(t, err)
	assert.False(t, on)
	grants, err := e.svc.UserGrants(ctx, owner)
	require.NoError(t, err)
	assert.Empty(t, grants)
	sealed, err := models.TOTPSecretOf(e.db, owner)
	require.NoError(t, err)
	assert.Empty(t, sealed)
}

func TestAnAdminResetNeedsNoCode(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enroll(t)
	_, err := e.svc.Reset(context.Background(), owner)
	require.NoError(t, err)
	on, err := e.svc.Enabled(owner)
	require.NoError(t, err)
	assert.False(t, on)
}

func TestLockEndsOneUnlockOfTheUsersOwnRadio(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	require.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: repeater}))
	e.clock.Advance(airauth.Period)
	require.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: hotspot, Slot2: true}))
	list, err := e.svc.UserGrants(ctx, owner)
	require.NoError(t, err)
	require.Len(t, list, 2)

	require.ErrorIs(t, e.svc.Lock(ctx, other, radio, repeater, 1), airauth.ErrNotYours)
	require.NoError(t, e.svc.Lock(ctx, owner, radio, repeater, 1))
	list, err = e.svc.UserGrants(ctx, owner)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, airauth.Grant{Radio: radio, Repeater: hotspot, Slot: 2, Until: e.clock.Now().Add(e.len)}, list[0])
}

func TestASecretOfAnotherKeyUnlocksNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret := e.enroll(t)
	e.key = airauth.KeyOf(&config.Config{Secret: "another-secret-15", PasswordSalt: "a-salt-of-fifteen!"})
	assert.Equal(t, airauth.Refused, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: e.code(secret, 0), Repeater: repeater}))
}

// failingKV fails the updates of the keys that start with prefix while
// prefix is set.
type failingKV struct {
	kv.KV
	prefix atomic.Pointer[string]
}

var errStoreDown = errors.New("store down")

func (f *failingKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	if p := f.prefix.Load(); p != nil && strings.HasPrefix(key, *p) {
		return errStoreDown
	}
	return f.KV.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

func TestATryThatCannotFinishIsLoggedAndNotCountedAsAWrongCode(t *testing.T) { //nolint:paralleltest // it replaces the default logger
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for _, prefix := range []string{"dmrhub:airauth:step:", "dmrhub:airauth:rate:in:", "dmrhub:airauth:grants:"} { //nolint:paralleltest // the subtests share the replaced default logger
		t.Run(prefix, func(t *testing.T) {
			e := newEnv(t)
			f := &failingKV{KV: e.store}
			e.store = f
			e.svc = e.service()
			secret := e.enroll(t)
			ctx := context.Background()
			code := e.code(secret, 0)
			logs.Reset()

			f.prefix.Store(&prefix)
			for range airauth.SourceTries + 2 {
				assert.Equal(t, airauth.Unavailable, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: code, Repeater: repeater}))
			}
			out := logs.String()
			assert.Contains(t, out, `msg="On-air code try could not finish" src=1000101 error=`)
			assert.Contains(t, out, "store down")
			assert.Contains(t, out, `msg="On-air code try" src=1000101 result=error`)
			assert.NotContains(t, out, fmt.Sprintf("%06d", code))
			assert.NotContains(t, out, fmt.Sprintf("9%06d", code))

			f.prefix.Store(nil)
			assert.Equal(t, airauth.Granted, e.svc.Attempt(ctx, airauth.Try{Src: radio, Owner: owner, Code: code, Repeater: repeater}),
				"tries that could not finish must not count against the limit of the radio")
		})
	}
}

// noExpireKV fails every Expire, as a store does that goes away between
// two calls.
type noExpireKV struct{ kv.KV }

func (noExpireKV) Expire(context.Context, string, time.Duration) error { return errStoreDown }

func TestASetupKeepsItsTimeLimitInOneWrite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.store = noExpireKV{KV: e.store}
	e.svc = e.service()
	ctx := context.Background()
	setup, err := e.svc.Begin(ctx, owner, "DMRHub", "n0code")
	require.NoError(t, err, "a setup must not need a second call to the store for its time limit")
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	require.NoError(t, err)
	e.clock.Advance(airauth.SetupTTL + time.Second)
	require.ErrorIs(t, e.svc.Confirm(ctx, owner, e.code(secret, 0)), airauth.ErrNoSetup, "the setup must end after its time limit")
}
