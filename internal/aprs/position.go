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

package aprs

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrPosition is returned for a position that cannot be reported.
var ErrPosition = errors.New("invalid APRS position")

const (
	knotsPerMPS   = 3600.0 / 1852.0
	feetPerMeter  = 1 / 0.3048
	maxKnots      = 999
	maxFeet       = 999999
	minFeet       = -99999
	extensionLen  = 7
	altitudeLen   = 9
	hundredthsDeg = 6000
	maxLatitude   = 90
	maxLongitude  = 180
)

// Report is a position report without a timestamp.
type Report struct {
	Latitude   float64
	Longitude  float64
	Symbol     Symbol
	HeadingDeg *float64
	SpeedMPS   *float64
	AltitudeM  *float64
	Comment    string
}

// Info returns the information field of r: the position, the course and
// speed when the speed is known, the altitude when it is known, and as much
// of the comment as fits.
func (r Report) Info() (string, error) {
	if math.IsNaN(r.Latitude) || math.IsNaN(r.Longitude) || math.Abs(r.Latitude) > 90 || math.Abs(r.Longitude) > 180 ||
		(r.Latitude == 0 && r.Longitude == 0) {
		return "", fmt.Errorf("%w: %v, %v", ErrPosition, r.Latitude, r.Longitude)
	}
	if _, ok := symbolName(r.Symbol); !ok {
		return "", fmt.Errorf("%w: symbol %q", ErrPosition, string([]byte{r.Symbol.Table, r.Symbol.Code}))
	}
	var b strings.Builder
	b.WriteByte('!')
	b.WriteString(coordinate(r.Latitude, 2, maxLatitude, 'N', 'S'))
	b.WriteByte(r.Symbol.Table)
	b.WriteString(coordinate(r.Longitude, 3, maxLongitude, 'E', 'W'))
	b.WriteByte(r.Symbol.Code)
	room := MaxComment
	if r.SpeedMPS != nil && *r.SpeedMPS >= 0 {
		course := 0
		if r.HeadingDeg != nil && *r.HeadingDeg >= 0 {
			course = int(math.Round(math.Mod(*r.HeadingDeg, 360)))
			if course == 0 {
				course = 360
			}
		}
		fmt.Fprintf(&b, "%03d/%03d", course, min(int(math.Round(*r.SpeedMPS*knotsPerMPS)), maxKnots))
		room -= extensionLen
	}
	if r.AltitudeM != nil {
		feet := min(max(int(math.Round(*r.AltitudeM*feetPerMeter)), minFeet), maxFeet)
		if feet < 0 {
			fmt.Fprintf(&b, "/A=-%05d", -feet)
		} else {
			fmt.Fprintf(&b, "/A=%06d", feet)
		}
		room -= altitudeLen
	}
	comment := r.Comment
	if !ValidComment(comment) {
		comment = ""
	}
	if len(comment) > room {
		comment = comment[:room]
	}
	b.WriteString(comment)
	return b.String(), nil
}

// coordinate writes v in degrees and hundredths of minutes, at most a
// hundredth of a minute short of most degrees, since parsers such as
// Ham::APRS::FAP refuse 90 degrees of latitude and 180 of longitude.
func coordinate(v float64, degDigits, most int, pos, neg byte) string {
	hemi := pos
	if v < 0 {
		hemi, v = neg, -v
	}
	h := min(int(math.Round(v*hundredthsDeg)), most*hundredthsDeg-1)
	return fmt.Sprintf("%0*d%02d.%02d%c", degDigits, h/hundredthsDeg, h%hundredthsDeg/100, h%100, hemi)
}

func symbolName(s Symbol) (string, bool) {
	for _, name := range SymbolNames() {
		if sym, _ := LookupSymbol(name); sym == s {
			return name, true
		}
	}
	return "", false
}
