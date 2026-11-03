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

package bubble

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gorm.io/gorm"
)

// Statements counts the statements of a database that run. SQLite waits
// for a lock with sleeps, and in a bubble a sleep blocks a goroutine
// durably, so synctest.Wait can return while a statement still waits for
// its lock and has not run yet.
type Statements struct {
	running atomic.Int64
}

// counted holds the Statements of each database that CountStatements
// counts, by the config that a database shares with its sessions.
var counted sync.Map //nolint:gochecknoglobals // the counters of the databases of all the tests of a process

// CountStatements returns a Statements for db, which SettleDB finds for db
// and its sessions until Forget. Call it before a goroutine uses db, since
// gorm registers callbacks only then.
func CountStatements(db *gorm.DB) (*Statements, error) {
	s := &Statements{}
	start := func(*gorm.DB) { s.running.Add(1) }
	end := func(*gorm.DB) { s.running.Add(-1) }
	cb := db.Callback()
	for _, r := range []struct {
		name       string
		start, end func(string, func(*gorm.DB)) error
	}{
		{"create", cb.Create().Before("*").Register, cb.Create().After("*").Register},
		{"query", cb.Query().Before("*").Register, cb.Query().After("*").Register},
		{"update", cb.Update().Before("*").Register, cb.Update().After("*").Register},
		{"delete", cb.Delete().Before("*").Register, cb.Delete().After("*").Register},
		{"row", cb.Row().Before("*").Register, cb.Row().After("*").Register},
		{"raw", cb.Raw().Before("*").Register, cb.Raw().After("*").Register},
	} {
		if err := r.start("bubble:start_"+r.name, start); err != nil {
			return nil, fmt.Errorf("count the %s statements: %w", r.name, err)
		}
		if err := r.end("bubble:end_"+r.name, end); err != nil {
			return nil, fmt.Errorf("count the %s statements: %w", r.name, err)
		}
	}
	counted.Store(db.Config, s)
	return s, nil
}

// Forget drops the Statements of db, such as when db closes.
func Forget(db *gorm.DB) {
	counted.Delete(db.Config)
}

// SettleDB is Statements.Settle for the Statements of db, and fails the
// test when no Statements counts the statements of db.
func SettleDB(t testing.TB, db *gorm.DB, within time.Duration) {
	t.Helper()
	v, _ := counted.Load(db.Config)
	s, ok := v.(*Statements)
	if !ok {
		t.Fatalf("no Statements counts the statements of the database")
	}
	s.Settle(t, within)
}

// Settle waits until every other goroutine of the bubble is durably
// blocked and no statement of the database runs, and fails the test when
// that does not happen within within.
func (s *Statements) Settle(t testing.TB, within time.Duration) {
	t.Helper()
	Eventually(t, func() bool {
		synctest.Wait()
		return s.running.Load() == 0
	}, within, time.Millisecond, "a statement of the database still runs")
}
