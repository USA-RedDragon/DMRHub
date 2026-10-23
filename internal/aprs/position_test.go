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
	"math"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f(v float64) *float64 { return &v }

const carCode = '>'

func TestAPositionReport(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		r    aprs.Report
		want string
	}{
		"plain":                       {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}}, "!3016.03N/09744.59W>"},
		"south east":                  {aprs.Report{Latitude: -33.8688, Longitude: 151.2093, Symbol: aprs.Symbol{Table: '/', Code: carCode}}, "!3352.13S/15112.56E>"},
		"rounds up to a whole degree": {aprs.Report{Latitude: 29.999999, Longitude: -97.9999999, Symbol: aprs.Symbol{Table: '/', Code: carCode}}, "!3000.00N/09800.00W>"},
		"date line":                   {aprs.Report{Latitude: 0.0001, Longitude: 179.99999, Symbol: aprs.Symbol{Table: '/', Code: carCode}}, "!0000.01N/17959.99E>"},
		"south pole":                  {aprs.Report{Latitude: -90, Longitude: -180, Symbol: aprs.Symbol{Table: '/', Code: carCode}}, "!8959.99S/17959.99W>"},
		"course and speed":            {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, HeadingDeg: f(45), SpeedMPS: f(4)}, "!3016.03N/09744.59W>045/008"},
		"north is 360":                {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, HeadingDeg: f(0.2), SpeedMPS: f(4)}, "!3016.03N/09744.59W>360/008"},
		"unknown course":              {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, SpeedMPS: f(0)}, "!3016.03N/09744.59W>000/000"},
		"heading alone is left out":   {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, HeadingDeg: f(90)}, "!3016.03N/09744.59W>"},
		"altitude":                    {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, AltitudeM: f(156)}, "!3016.03N/09744.59W>/A=000512"},
		"below sea level":             {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, AltitudeM: f(-30)}, "!3016.03N/09744.59W>/A=-00098"},
		"comment":                     {aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: '['}, Comment: "DMR 3191868"}, "!3016.03N/09744.59W[DMR 3191868"},
		"everything": {
			aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, HeadingDeg: f(270), SpeedMPS: f(30), AltitudeM: f(200), Comment: "DMR 3191868"},
			"!3016.03N/09744.59W>270/058/A=000656DMR 3191868",
		},
	} {
		got, err := tc.r.Info()
		require.NoError(t, err, name)
		assert.Equal(t, tc.want, got, name)
	}
}

func TestTheCommentIsCutToFit(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("c", aprs.MaxComment)
	got, err := aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, Comment: long}.Info()
	require.NoError(t, err)
	assert.Equal(t, "!3016.03N/09744.59W>"+long, got)
	got, err = aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, SpeedMPS: f(1), AltitudeM: f(1), Comment: long}.Info()
	require.NoError(t, err)
	assert.Equal(t, "!3016.03N/09744.59W>000/002/A=000003"+long[:aprs.MaxComment-16], got)
	got, err = aprs.Report{Latitude: 30.2672, Longitude: -97.7431, Symbol: aprs.Symbol{Table: '/', Code: carCode}, Comment: "a|b"}.Info()
	require.NoError(t, err)
	assert.Equal(t, "!3016.03N/09744.59W>", got, "a comment that cannot go out is left out")
}

func TestPositionsThatCannotBeReported(t *testing.T) {
	t.Parallel()
	for name, r := range map[string]aprs.Report{
		"0, 0":      {Latitude: 0, Longitude: 0, Symbol: aprs.Symbol{Table: '/', Code: carCode}},
		"latitude":  {Latitude: 91, Longitude: 0, Symbol: aprs.Symbol{Table: '/', Code: carCode}},
		"longitude": {Latitude: 10, Longitude: -181, Symbol: aprs.Symbol{Table: '/', Code: carCode}},
		"nan":       {Latitude: math.NaN(), Longitude: 10, Symbol: aprs.Symbol{Table: '/', Code: carCode}},
		"symbol":    {Latitude: 10, Longitude: 10, Symbol: aprs.Symbol{Table: '/', Code: 'Z'}},
	} {
		_, err := r.Info()
		assert.ErrorIs(t, err, aprs.ErrPosition, name)
	}
}
