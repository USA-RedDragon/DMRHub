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

package liveconfig

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"gorm.io/gorm"
)

// ErrNeedsDatabase is a change in the setup wizard of a setting other than
// the database, before DMRHub reaches its database.
var ErrNeedsDatabase = errors.New("DMRHub can change this setting once it reaches its database; fix the database first")

// ErrDatabaseFixed is a second fix of the database setting.
var ErrDatabaseFixed = errors.New("the database setting is already fixed; reload the page")

// DatabaseFix is the start of DMRHub while it cannot reach its database.
// The setup wizard tries a new database setting with Fix, and the start
// takes the database that works from Fixed.
type DatabaseFix struct {
	Base  *config.Config
	Locks Locks
	State DatabaseState
	// Open connects to the database of a config and takes its migration
	// lock, as OpenDatabase does, and returns the error of its context once
	// the context ends.
	Open func(context.Context, *config.Config) (Migration, error)
	// Probe connects to the database of a config and closes it again, as
	// db.Ping does.
	Probe func(context.Context, *config.Config) error
	// Err is why DMRHub cannot reach its database.
	Err error

	mu       sync.Mutex
	done     bool
	status   FixStatus
	fixed    chan Fixed
	usedFile bool
}

// Fixed is a database that the setup wizard reached. The start of DMRHub
// calls Done with the phase of the wizard that comes next.
type Fixed struct {
	DB     *gorm.DB
	Config *config.Config
	ack    chan string
}

// Done tells the setup wizard which phase comes next: PhaseSetup, or
// PhaseDone when DMRHub needs no setup.
func (f Fixed) Done(phase string) {
	f.ack <- phase
}

const (
	// PhaseDatabase is the setup wizard while DMRHub cannot reach its
	// database.
	PhaseDatabase = "database"
	// PhaseSetup is the setup wizard while it sets the settings and the
	// first admin.
	PhaseSetup = "setup"
	// PhaseDone is the end of the setup wizard; DMRHub starts.
	PhaseDone = "done"
)

// NewDatabaseFix returns a DatabaseFix for a base config whose database
// DMRHub cannot reach because of err.
func NewDatabaseFix(base *config.Config, locks Locks, state DatabaseState, open func(context.Context, *config.Config) (Migration, error), err error) *DatabaseFix {
	return &DatabaseFix{Base: base, Locks: locks, State: state, Open: open, Probe: db.Ping, Err: err, fixed: make(chan Fixed), usedFile: SQLiteFileUsed(base.Database), status: FixStatus{State: FixIdle}}
}

// Migration is a database whose migration lock is held. Migrate migrates
// it and returns it, or closes it and returns an error. The end of the
// context of Migrate does not stop a migration.
type Migration interface {
	Migrate(ctx context.Context) (*gorm.DB, error)
}

// OpenDatabase connects to the database of a config and takes its
// migration lock with db.Connect.
func OpenDatabase(ctx context.Context, cfg *config.Config) (Migration, error) {
	c, err := db.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	return c, nil
}

// FixState is the state of a Fix.
type FixState string

const (
	// FixIdle is a DatabaseFix that has not run a Fix.
	FixIdle FixState = "idle"
	// FixPending is a Fix that runs.
	FixPending FixState = "pending"
	// FixDone is a Fix whose database the start of DMRHub took.
	FixDone FixState = "done"
	// FixFailed is a Fix that ended with an error.
	FixFailed FixState = "failed"
)

// FixStatus is the state of the last Fix: pending while it runs, done
// with the phase of the setup wizard that comes next, or failed with why.
type FixStatus struct {
	State FixState
	// Phase is the phase of the setup wizard that comes next, once the
	// Fix is done.
	Phase string
	// Err is why the Fix failed.
	Err error
	// Database is the database setting that the Fix tries.
	Database config.Database
}

// Fixed returns the channel of the database that works.
func (f *DatabaseFix) Fixed() <-chan Fixed {
	return f.fixed
}

// Problem is why DMRHub cannot reach its database, and whether the setup
// wizard can fix it.
type Problem struct {
	Error string `json:"database_error"`
	// Locked is whether the deployment sets the database, so that only the
	// deployment can fix it.
	Locked bool `json:"database_locked"`
	// Saved is whether a database setting was saved before, so that only
	// the deployment can change it.
	Saved bool `json:"database_saved"`
	// File is where the wizard keeps the setting it fixes, and StoreError
	// why it cannot write there.
	File       string `json:"database_file"`
	StoreError string `json:"store_error,omitempty"`
}

// Problem returns why DMRHub cannot reach its database.
func (f *DatabaseFix) Problem() Problem {
	p := Problem{File: f.State.Path, Locked: DatabaseLocked(f.Locks), Saved: f.Closed()}
	if f.Err != nil {
		p.Error = f.Err.Error()
	}
	if err := f.State.Writable(); err != nil {
		p.StoreError = err.Error()
	}
	return p
}

// Closed reports whether a database setting was saved before, or the SQLite
// file of the base config has a user, and the deployment sets none, so that
// the setup wizard cannot change the database.
func (f *DatabaseFix) Closed() bool {
	return !DatabaseLocked(f.Locks) && f.saved()
}

func (f *DatabaseFix) saved() bool {
	return f.usedFile || f.State.Saved()
}

// Check returns why next cannot be tried: a change of a setting other than
// the database, or of a database setting that the deployment sets.
func (f *DatabaseFix) Check(next *config.Config) []config.ValidationError {
	var errs []config.ValidationError
	locked := DatabaseLocked(f.Locks)
	closed := f.Closed()
	for _, k := range Keys() {
		if k.Equal(next, f.Base) {
			continue
		}
		switch {
		case !under(k.Path, "database"):
			errs = append(errs, config.ValidationError{Field: k.Path, Err: ErrNeedsDatabase})
		case locked:
			errs = append(errs, config.ValidationError{Field: k.Path, Err: ErrDatabaseLocked})
		case closed:
			errs = append(errs, config.ValidationError{Field: k.Path, Err: ErrDatabaseSaved})
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return next.Database.ValidateWithFields()
}

// FixTimeout bounds the start of a Fix: the connection to the database
// and the wait for its migration lock. It stays below the 10 s read and
// write timeouts of the setup wizard server, so that the answer of Fix
// reaches the browser.
const FixTimeout = 8 * time.Second

// HandOffTimeout bounds each wait of a Fix for the start of DMRHub: to take
// the database, then to name the phase that comes next.
const HandOffTimeout = time.Minute

// Status returns the state of the last Fix.
func (f *DatabaseFix) Status() FixStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// Fix tries the database setting next. It connects to the database and
// takes its migration lock, and gives up when ctx ends or after
// FixTimeout. Once the migration lock is held, the migration runs to its
// end whatever ctx does, and Fix returns the pending status at once. When
// the migration works, the fix keeps next in the DatabaseState, hands the
// database to the start of DMRHub, and Status reports the phase that
// comes next. While a Fix runs, Fix returns its status and tries nothing.
func (f *DatabaseFix) Fix(ctx context.Context, next config.Database) (FixStatus, error) {
	f.mu.Lock()
	switch {
	case f.status.State == FixPending:
		defer f.mu.Unlock()
		return f.status, nil
	case f.done:
		f.mu.Unlock()
		return FixStatus{}, ErrDatabaseFixed
	case DatabaseLocked(f.Locks):
		f.mu.Unlock()
		return FixStatus{}, ErrDatabaseLocked
	case f.saved():
		f.mu.Unlock()
		return FixStatus{}, ErrDatabaseSaved
	}
	if err := f.State.Writable(); err != nil {
		f.mu.Unlock()
		return FixStatus{}, fmt.Errorf("the setup wizard cannot keep a database setting, so set the database in the config file or the environment of the deployment: %w", err)
	}
	f.status = FixStatus{State: FixPending, Database: next}
	f.mu.Unlock()

	cfg := *f.Base
	cfg.Database = next
	connectCtx, cancel := context.WithTimeout(ctx, FixTimeout)
	defer cancel()
	m, err := f.Open(connectCtx, &cfg)
	if err != nil {
		err = fmt.Errorf("DMRHub cannot reach this database: %w", err)
		return f.finish(FixStatus{State: FixFailed, Err: err, Database: next}), err
	}
	go f.migrate(context.WithoutCancel(ctx), m, &cfg)
	return f.Status(), nil
}

// Try connects to the database of the setting next and closes it again,
// and gives up when ctx ends or after FixTimeout. It keeps nothing and
// migrates nothing, and refuses a setting that Fix would refuse.
func (f *DatabaseFix) Try(ctx context.Context, next config.Database) error {
	f.mu.Lock()
	saved := f.saved()
	f.mu.Unlock()
	switch {
	case DatabaseLocked(f.Locks):
		return ErrDatabaseLocked
	case saved:
		return ErrDatabaseSaved
	}
	cfg := *f.Base
	cfg.Database = next
	ctx, cancel := context.WithTimeout(ctx, FixTimeout)
	defer cancel()
	if err := f.Probe(ctx, &cfg); err != nil {
		return fmt.Errorf("DMRHub cannot reach this database: %w", err)
	}
	return nil
}

func (f *DatabaseFix) finish(s FixStatus) FixStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
	return s
}

func (f *DatabaseFix) migrate(ctx context.Context, m Migration, cfg *config.Config) {
	next := cfg.Database
	database, err := m.Migrate(ctx)
	if err != nil {
		f.finish(FixStatus{State: FixFailed, Err: fmt.Errorf("DMRHub cannot migrate this database: %w", err), Database: next})
		return
	}
	if err := f.State.Save(next); err != nil {
		closeDB(database)
		f.finish(FixStatus{State: FixFailed, Err: fmt.Errorf("DMRHub reached the database but cannot keep the setting, so set it in the config file or the environment of the deployment: %w", err), Database: next})
		return
	}
	f.mu.Lock()
	f.done = true
	f.mu.Unlock()
	ack := make(chan string, 1)
	take := time.NewTimer(HandOffTimeout)
	defer take.Stop()
	select {
	case f.fixed <- Fixed{DB: database, Config: cfg, ack: ack}:
	case <-take.C:
		closeDB(database)
		f.finish(FixStatus{State: FixFailed, Err: errors.New("the start of DMRHub did not take the database; restart DMRHub"), Database: next})
		return
	}
	phase := PhaseSetup
	answer := time.NewTimer(HandOffTimeout)
	defer answer.Stop()
	select {
	case phase = <-ack:
	case <-answer.C:
	}
	f.finish(FixStatus{State: FixDone, Phase: phase, Database: next})
}

func closeDB(database *gorm.DB) {
	if sqlDB, err := database.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
