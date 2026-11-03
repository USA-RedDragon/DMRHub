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

package hub_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func registerStatements(t testing.TB, database *gorm.DB) {
	t.Helper()
	_, err := bubble.CountStatements(database)
	require.NoError(t, err)
	t.Cleanup(func() { bubble.Forget(database) })
}

// settleDB waits until the hub of a test in a bubble is durably blocked
// with no statement of database running.
func settleDB(t testing.TB, database *gorm.DB) {
	t.Helper()
	bubble.SettleDB(t, database, failAfter)
}
