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

package testutils

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheTestsOfAProcessShareOneServerOfEachMySQLFamily(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() { _ = closeShared(context.Background()) })
	for _, base := range []*mysqlFamily{mysqlServer, mariadbServer} {
		t.Run(base.name, func(t *testing.T) {
			t.Parallel()
			f := &mysqlFamily{name: base.name, image: base.image, tag: base.tag, envPrefix: base.envPrefix, fast: base.fast}
			const tests = 4
			ports := make([]int, tests)
			port, name := f.shared(t)
			ports[0] = port
			db, err := sql.Open("mysql", mysqlDSN(port, name))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			conn, err := db.Conn(t.Context())
			require.NoError(t, err)
			for i := 1; i < tests; i++ {
				t.Run("", func(t *testing.T) { //nolint:paralleltest // each ends, with its cleanup, before the next starts
					ports[i], _ = f.shared(t)
				})
			}
			_, err = conn.ExecContext(t.Context(), "SELECT 1")
			assert.NoError(t, err, "the cleanup of the other tests ended no session of this test")
			_ = conn.Close()
			for i := range ports {
				assert.Equal(t, ports[0], ports[i], "the tests run on one %s server", f.name)
			}
		})
	}
}

func TestTheRaceDetectorRunsOneParallelTestPerCPU(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 32, parallelTests(8, 4, false))
	assert.Zero(t, parallelTests(8, 4, true), "the race detector keeps the default of go test")
	assert.Zero(t, parallelTests(0, 4, false))
}
