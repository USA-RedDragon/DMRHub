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
	"sync"
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
			var acquired, released sync.WaitGroup
			acquired.Add(tests)
			released.Add(tests - 1)
			ports := make([]int, tests)
			t.Run("group", func(t *testing.T) {
				for i := range tests {
					t.Run("", func(t *testing.T) {
						t.Parallel()
						if i > 0 {
							t.Cleanup(released.Done)
						}
						port, name := f.shared(t)
						ports[i] = port
						acquired.Done()
						acquired.Wait()
						if i > 0 {
							return
						}
						db, err := sql.Open("mysql", mysqlDSN(port, name))
						require.NoError(t, err)
						t.Cleanup(func() { _ = db.Close() })
						conn, err := db.Conn(t.Context())
						require.NoError(t, err)
						released.Wait()
						_, err = conn.ExecContext(t.Context(), "SELECT 1")
						assert.NoError(t, err, "the cleanup of the other tests ended no session of this test")
						_ = conn.Close()
					})
				}
			})
			for i := range ports {
				assert.Equal(t, ports[0], ports[i], "the tests run on one %s server", f.name)
			}
		})
	}
}
