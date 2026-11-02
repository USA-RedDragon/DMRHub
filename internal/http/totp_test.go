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

package http_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type onAirState struct {
	On            bool  `json:"on"`
	UnlockSeconds int64 `json:"unlock_seconds"`
	Unlocks       []struct {
		Radio    uint   `json:"radio"`
		Repeater uint   `json:"repeater"`
		Callsign string `json:"callsign"`
		Slot     uint8  `json:"slot"`
		Until    string `json:"until"`
	} `json:"unlocks"`
}

const (
	onAirUser     = uint(1000201)
	onAirRadio    = uint(1000202)
	onAirOther    = uint(1000203)
	onAirRepeater = uint(100201)
)

// newOnAirEnv returns a router with two users, the session of the first,
// and the time at which it holds the clock of the on-air codes still.
func newOnAirEnv(t *testing.T) (*oauthEnv, testutils.CookieJar, time.Time) {
	t.Helper()
	e := newOAuthEnv(t)
	e.tdb.Hub().SetConfig(config.Config{Secret: "a-secret-of-fifteen", PasswordSalt: "a-salt-of-fifteen!"})
	now := time.Now()
	e.tdb.Hub().SetUnlockClock(clock.NewFake(now))
	db := e.tdb.DB()
	testutils.SeedAccount(t, db, onAirUser, "N0CODE", "n0code")
	testutils.SeedAccount(t, db, onAirOther, "N1CODE", "n1code")
	require.NoError(t, db.Create(&models.UserRadioID{UserID: onAirUser, RadioID: onAirRadio, VerifiedAt: time.Now()}).Error)
	require.NoError(t, db.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: onAirRepeater, Callsign: "N0CODE"}, OwnerID: onAirUser, Type: models.RepeaterTypeMMDVM}).Error)
	return e, testutils.Login(t, e.router, "n0code"), now
}

func readOnAir(t *testing.T, e *oauthEnv, jar testutils.CookieJar) onAirState {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/users/me/totp", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s onAirState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func setUpOnAir(t *testing.T, e *oauthEnv, jar testutils.CookieJar) []byte {
	t.Helper()
	w := testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp/setup", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var setup struct{ Key, URI, QR string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	assert.True(t, strings.HasPrefix(setup.QR, "data:image/png;base64,"), setup.QR[:min(len(setup.QR), 40)])
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(setup.QR, "data:image/png;base64,"))
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(raw))
	require.NoError(t, err)
	b := img.Bounds()
	require.GreaterOrEqual(t, b.Dx(), 200, "the QR code must be big enough to scan")
	dark := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
				dark++
			}
		}
	}
	assert.Greater(t, dark, b.Dx()*b.Dy()/5, "the modules must fill the image, not one corner of it")
	assert.True(t, strings.HasPrefix(setup.URI, "otpauth://totp/DMRHub:N0CODE?"), setup.URI)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	require.NoError(t, err)
	return secret
}

func stepCode(secret []byte, now time.Time, d int64) string {
	return fmt.Sprintf("%06d", airauth.Code(secret, airauth.Step(now)+d))
}

func TestTheOnAirCodeTurnsOnWithACodeAndKeepsItsSecretHidden(t *testing.T) {
	t.Parallel()
	e, jar, now := newOnAirEnv(t)
	assert.False(t, readOnAir(t, e, jar).On)
	secret := setUpOnAir(t, e, jar)
	assert.False(t, readOnAir(t, e, jar).On, "a setup alone must not turn the code on")

	for _, bad := range []map[string]any{{"code": "12345"}, {"code": "abcdef"}, {}} {
		w := testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%v", bad)
	}
	w := testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, 3)})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w = testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, 0)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	state := readOnAir(t, e, jar)
	assert.True(t, state.On)
	assert.Equal(t, int64(24*3600), state.UnlockSeconds)

	w = testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/users/me/totp", nil)
	keys := testutils.JSONKeys(t, w.Body.Bytes())
	assert.False(t, keys["key"])
	assert.False(t, keys["secret"])
	assert.NotContains(t, w.Body.String(), airauth.EncodeSecret(secret))
	me := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/users/me", nil)
	assert.NotContains(t, me.Body.String(), "totp")
	assert.NotContains(t, me.Body.String(), "v1:")

	sealed, err := models.TOTPSecretOf(e.tdb.DB(), onAirUser)
	require.NoError(t, err)
	assert.NotEmpty(t, sealed)
	assert.NotContains(t, sealed, airauth.EncodeSecret(secret))

	w = testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp/setup", nil)
	assert.Equal(t, http.StatusConflict, w.Code, "a second setup must not replace the secret")
}

func TestTheOnAirCodeIsForTheSignedInUserOnly(t *testing.T) {
	t.Parallel()
	e, jar, _ := newOnAirEnv(t)
	made := makeToken(t, e, jar, map[string]any{"name": "All", "full": true, "expires_in_days": 30})
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/totp"},
		{http.MethodPost, "/api/v1/users/me/totp/setup"},
		{http.MethodPost, "/api/v1/users/me/totp"},
		{http.MethodDelete, "/api/v1/users/me/totp"},
		{http.MethodDelete, "/api/v1/users/me/totp/unlocks/1000202/100201/1"},
	} {
		assert.Equal(t, http.StatusForbidden, e.bearer(t, r.method, r.path, made.Secret).Code, "%s %s with a full access token", r.method, r.path)
	}
	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, e.router, testutils.CookieJar{}, http.MethodGet, "/api/v1/users/me/totp", nil).Code)
}

func TestTheUnlocksAreListedAndLockedOneByOne(t *testing.T) {
	t.Parallel()
	e, jar, now := newOnAirEnv(t)
	secret := setUpOnAir(t, e, jar)
	require.Equal(t, http.StatusOK, testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, -1)}).Code)
	svc := e.tdb.Hub().OnAirCodes()
	ctx := context.Background()
	require.Equal(t, airauth.Granted, svc.Attempt(ctx, airauth.Try{Src: onAirRadio, Owner: onAirUser, Code: airauth.Code(secret, airauth.Step(now)), Repeater: onAirRepeater, Slot2: true}))
	require.Equal(t, airauth.Granted, svc.Attempt(ctx, airauth.Try{Src: onAirUser, Owner: onAirUser, Code: airauth.Code(secret, airauth.Step(now)+1), Repeater: 100202}))

	state := readOnAir(t, e, jar)
	require.Len(t, state.Unlocks, 2)
	byRadio := map[uint]int{}
	for i, u := range state.Unlocks {
		byRadio[u.Radio] = i
	}
	u := state.Unlocks[byRadio[onAirRadio]]
	assert.Equal(t, onAirRepeater, u.Repeater)
	assert.Equal(t, "N0CODE", u.Callsign)
	assert.Equal(t, uint8(2), u.Slot)

	other := testutils.Login(t, e.router, "n1code")
	w := testutils.Do(t, e.router, other, http.MethodDelete, "/api/v1/users/me/totp/unlocks/1000202/100201/2", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "a user cannot lock the radio of another")
	assert.Len(t, readOnAir(t, e, jar).Unlocks, 2)

	w = testutils.Do(t, e.router, jar, http.MethodDelete, "/api/v1/users/me/totp/unlocks/1000202/100201/2", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	state = readOnAir(t, e, jar)
	require.Len(t, state.Unlocks, 1)
	assert.Equal(t, onAirUser, state.Unlocks[0].Radio)
	assert.Equal(t, http.StatusBadRequest, testutils.Do(t, e.router, jar, http.MethodDelete, "/api/v1/users/me/totp/unlocks/1000202/100201/3", nil).Code)
}

func TestTurningTheOnAirCodeOffNeedsACodeOrAnAdmin(t *testing.T) {
	t.Parallel()
	e, jar, now := newOnAirEnv(t)
	secret := setUpOnAir(t, e, jar)
	require.Equal(t, http.StatusOK, testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, -1)}).Code)

	w := testutils.Do(t, e.router, jar, http.MethodDelete, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, -1)})
	assert.Equal(t, http.StatusBadRequest, w.Code, "a used code must not turn it off")
	w = testutils.Do(t, e.router, jar, http.MethodDelete, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, 0)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, readOnAir(t, e, jar).On)

	secret = setUpOnAir(t, e, jar)
	require.Equal(t, http.StatusOK, testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, 1)}).Code)
	other := testutils.Login(t, e.router, "n1code")
	assert.Equal(t, http.StatusForbidden, testutils.Do(t, e.router, other, http.MethodDelete, "/api/v1/users/1000201/totp", nil).Code)
	assert.True(t, readOnAir(t, e, jar).On)
	w = testutils.Do(t, e.router, e.jar, http.MethodDelete, "/api/v1/users/1000201/totp", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, readOnAir(t, e, jar).On, "an admin turns it off for a user who lost their app")
}

func TestTheUserListTellsAdminsWhoseOnAirCodeIsOn(t *testing.T) {
	t.Parallel()
	e, jar, now := newOnAirEnv(t)
	secret := setUpOnAir(t, e, jar)
	require.Equal(t, http.StatusOK, testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/users/me/totp", map[string]any{"code": stepCode(secret, now, 0)}).Code)
	w := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		OnAir []uint `json:"on_air_codes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, []uint{onAirUser}, list.OnAir)
	assert.NotContains(t, w.Body.String(), "totp_secret")
	assert.NotContains(t, w.Body.String(), "v1:")
}
