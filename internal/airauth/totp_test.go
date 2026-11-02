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
	"net/url"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rfcKey() []byte { return []byte("12345678901234567890") }

func TestCodeMatchesTheSHA1VectorsOfRFC6238(t *testing.T) {
	t.Parallel()
	for unix, want := range map[int64]uint32{
		59: 287082, 1111111109: 81804, 1111111111: 50471, 1234567890: 5924, 2000000000: 279037, 20000000000: 353130,
	} {
		assert.Equal(t, want, airauth.Code(rfcKey(), airauth.Step(time.Unix(unix, 0))), "time %d", unix)
	}
}

func TestMatchTakesOnlyTheStepsBeforeNowAndAfter(t *testing.T) {
	t.Parallel()
	now := time.Unix(1234567890, 0)
	step := airauth.Step(now)
	for d := int64(-3); d <= 3; d++ {
		got, ok := airauth.Match(rfcKey(), airauth.Code(rfcKey(), step+d), now)
		want := d >= -1 && d <= 1
		assert.Equal(t, want, ok, "step %+d", d)
		if want {
			assert.Equal(t, step+d, got, "step %+d", d)
		}
	}
}

func TestCodeOfCallTakesNineAndSixDigits(t *testing.T) {
	t.Parallel()
	code, ok := airauth.CodeOfCall(9005924)
	require.True(t, ok)
	assert.Equal(t, uint32(5924), code)
	for _, dst := range []uint{5924, 905924, 8999999, 10000000} {
		_, ok := airauth.CodeOfCall(dst)
		assert.False(t, ok, "%d", dst)
	}
}

func TestParseCodeTakesSixDigits(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]uint32{"005924": 5924, "482 913": 482913, " 999999 ": 999999} {
		got, ok := airauth.ParseCode(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "5924", "1234567", "12a456", "-12345", "１２３４５６"} {
		_, ok := airauth.ParseCode(in)
		assert.False(t, ok, in)
	}
}

func TestURIHasTheSecretAndTheIssuer(t *testing.T) {
	t.Parallel()
	u, err := url.Parse(airauth.URI("DMRHub", "N0CALL", rfcKey()))
	require.NoError(t, err)
	assert.Equal(t, "otpauth", u.Scheme)
	assert.Equal(t, "totp", u.Host)
	assert.Equal(t, "/DMRHub:N0CALL", u.Path)
	assert.Equal(t, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", u.Query().Get("secret"))
	assert.Equal(t, "DMRHub", u.Query().Get("issuer"))
	assert.Equal(t, "6", u.Query().Get("digits"))
	assert.Equal(t, "30", u.Query().Get("period"))
}

func TestASealedSecretOpensOnlyWithItsKeyAndUser(t *testing.T) {
	t.Parallel()
	key := airauth.KeyOf(&config.Config{Secret: "a-secret-of-fifteen", PasswordSalt: "a-salt-of-fifteen!"})
	require.Len(t, key, 32)
	sealed, err := airauth.Seal(key, 1000001, rfcKey())
	require.NoError(t, err)
	assert.NotContains(t, sealed, airauth.EncodeSecret(rfcKey()))
	assert.NotContains(t, sealed, string(rfcKey()))

	got, err := airauth.Open(key, 1000001, sealed)
	require.NoError(t, err)
	assert.Equal(t, rfcKey(), got)

	_, err = airauth.Open(key, 1000002, sealed)
	require.ErrorIs(t, err, airauth.ErrSealed)
	other := airauth.KeyOf(&config.Config{Secret: "another-secret-15", PasswordSalt: "a-salt-of-fifteen!"})
	_, err = airauth.Open(other, 1000001, sealed)
	require.ErrorIs(t, err, airauth.ErrSealed)
	_, err = airauth.Open(nil, 1000001, sealed)
	require.ErrorIs(t, err, airauth.ErrSealed)
	_, err = airauth.Open(key, 1000001, "v1:"+sealed[3:10])
	require.ErrorIs(t, err, airauth.ErrSealed)

	again, err := airauth.Seal(key, 1000001, rfcKey())
	require.NoError(t, err)
	assert.NotEqual(t, sealed, again, "each seal must use a new nonce")
}
