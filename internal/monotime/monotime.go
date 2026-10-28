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

// Package monotime keeps a time in an int64 with its monotonic clock
// reading, so that a step of the wall clock does not change how long ago
// it was. time.Time.UnixNano drops that reading. The epoch of the stamps
// is a century before the process started, so that the stamp of a time
// after 1926 is positive, also in a synctest bubble, whose clock starts in
// 2000 with no monotonic clock reading.
package monotime

import "time"

const century = 100 * 365 * 24 * time.Hour

var epoch = time.Now().Add(-century) //nolint:gochecknoglobals

// Now returns the stamp of the current time.
func Now() int64 {
	return int64(time.Since(epoch))
}

// Of returns the stamp of t, which must carry a monotonic clock reading,
// as the times of time.Now do.
func Of(t time.Time) int64 {
	return int64(t.Sub(epoch))
}

// Time returns the time of the stamp s, with a monotonic clock reading.
func Time(s int64) time.Time {
	return epoch.Add(time.Duration(s))
}
