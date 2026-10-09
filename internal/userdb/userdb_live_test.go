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

//go:build live

package userdb

import (
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/retry"
)

const liveUserDBURL = "https://www.radioid.net/static/users.json"

// TestLiveUpdate downloads and decodes the current RadioID.net user dump.
// It needs the live build tag and runs in the weekly RadioID.net refresh workflow,
// so a format change fails that workflow instead of every CI run.
func TestLiveUpdate(t *testing.T) {
	t.Parallel()
	db := newFixtureDB(t)
	backoff := retry.Backoff{Attempts: 5, Initial: 5 * time.Second, Max: time.Minute}
	backoff.Run(t, func(r *retry.R) {
		if err := db.Update(liveUserDBURL); err != nil {
			r.Errorf("Update failed: %v", err)
		}
	})
	if t.Failed() {
		return
	}
	if db.Len() < 231290 {
		t.Errorf("live user database looks truncated, found %d users", db.Len())
	}
	user, ok := db.Get(3191868)
	if !ok || !strings.EqualFold(user.Callsign, "KI5VMF") {
		t.Errorf("unexpected live user 3191868: %+v", user)
	}
}
