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

package aprs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLine(t *testing.T) {
	t.Parallel()
	c := aprs.Config{Login: "N0HUB-10", Passcode: "12345", Server: "rotate.aprs2.net:14580"}
	line, err := c.Line("KI5VMF-9", "KI5VMF", "!3016.03N/09744.62W>")
	require.NoError(t, err)
	assert.Equal(t, "KI5VMF-9>APZDMH,KI5VMF*,qAR,N0HUB-10:!3016.03N/09744.62W>", line)
}

func TestEveryLineGoesToTheToCall(t *testing.T) {
	t.Parallel()
	configs := []aprs.Config{
		{},
		{Login: "N0HUB-10", Passcode: "12345", Server: "rotate.aprs2.net:14580"},
		{Login: "APRS", Passcode: "-1", Server: "APDHXX"},
		{Login: "APBM1S", Passcode: "APBM1S", Server: "APBM1S:14580"},
	}
	packets := []struct{ source, via, info string }{
		{"KI5VMF-9", "KI5VMF", "!3016.03N/09744.62W>"},
		{"DL1ABC-7", "DL0HB", "=5230.00N/01324.00E[ comment"},
		{"N0CALL", "APZXYZ", "!0000.00N\\00000.00Wk"},
	}
	for _, c := range configs {
		for _, p := range packets {
			line, err := c.Line(p.source, p.via, p.info)
			if c.Login == "" {
				require.ErrorIs(t, err, aprs.ErrCallsign)
				continue
			}
			require.NoError(t, err)
			_, rest, _ := strings.Cut(line, ">")
			dest, _, _ := strings.Cut(rest, ",")
			assert.Equal(t, "APZDMH", dest, "%+v %+v", c, p)
		}
	}
}

func TestNoConfigFieldNamesTheToCall(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[aprs.Config]()
	for i := range typ.NumField() {
		name := strings.ToLower(typ.Field(i).Name)
		assert.NotContains(t, name, "tocall")
		assert.NotContains(t, name, "dest")
	}
}

func TestLineRefusesInvalidCallsigns(t *testing.T) {
	t.Parallel()
	c := aprs.Config{Login: "N0HUB-10"}
	for _, call := range []string{"", "ki5vmf", "KI5VMF-", "KI5VMF-100", "KI5VMF>APZXXX", "KI5,VMF", "KI5VMF:X", "TOOLONGCALL"} {
		_, err := c.Line(call, "KI5VMF", "!")
		assert.ErrorIs(t, err, aprs.ErrCallsign, call)
	}
	_, err := c.Line("KI5VMF", "KI5VMF", "!\r\nN0CALL>APRS:x")
	assert.Error(t, err)
}

func TestLicensedCallsigns(t *testing.T) {
	t.Parallel()
	for _, call := range []string{"KI5VMF", "N0CALL", "VK2ABC", "2E0ABC"} {
		assert.True(t, aprs.Licensed(call), call)
	}
	for _, call := range []string{"", "GUEST", "123456", "KI5VMF-9", "ki5vmf", "KI5 VMF", "ABCDEFGHI1"} {
		assert.False(t, aprs.Licensed(call), call)
	}
}

func TestThePasscodeComesFromTheCallsign(t *testing.T) {
	t.Parallel()
	for login, want := range map[string]string{
		"N0CALL":    "13023",
		"KI5VMF-10": "16571",
		"ki5vmf":    "16571",
		"N0HUB-10":  "14215",
	} {
		assert.Equal(t, want, aprs.Passcode(login), login)
	}
}
