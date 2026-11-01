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

package calltracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARecordWithANullPartLoads loads a shared record whose part is null,
// as a damaged or hand-edited KV entry holds. The record loads without the
// part, and a claim on it does not panic.
func TestARecordWithANullPartLoads(t *testing.T) {
	t.Parallel()
	rec := loadRecord([]byte(`{"id":5,"chan":"c","parts":{"x":null}}`))
	require.NotNil(t, rec)
	assert.Empty(t, rec.Parts)
	assert.NotPanics(t, func() { claimRecord(rec, "me", "c", 1, false, time.Now()) })
}
