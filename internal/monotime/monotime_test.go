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

package monotime_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/monotime"
	"github.com/stretchr/testify/assert"
)

func TestAStampKeepsTheMonotonicReading(t *testing.T) {
	t.Parallel()
	now := time.Now()
	at := monotime.Time(monotime.Of(now))
	assert.Equal(t, time.Duration(0), at.Sub(now))
	assert.True(t, at.Equal(now))
	assert.Contains(t, at.String(), " m=+", "the time of a stamp has a monotonic clock reading")
}

func TestNowIsAfterAnEarlierStamp(t *testing.T) {
	t.Parallel()
	before := monotime.Now()
	time.Sleep(time.Millisecond)
	assert.Greater(t, monotime.Now(), before)
	assert.Less(t, time.Since(monotime.Time(before)), time.Minute)
}
