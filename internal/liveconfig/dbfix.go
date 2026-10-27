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

	"github.com/USA-RedDragon/DMRHub/internal/config"
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
	// Open connects to the database of a config, as db.MakeDB does.
	Open func(*config.Config) (*gorm.DB, error)
	// Err is why DMRHub cannot reach its database.
	Err error

	mu    sync.Mutex
	done  bool
	fixed chan Fixed
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
func NewDatabaseFix(base *config.Config, locks Locks, state DatabaseState, open func(*config.Config) (*gorm.DB, error), err error) *DatabaseFix {
	return &DatabaseFix{Base: base, Locks: locks, State: state, Open: open, Err: err, fixed: make(chan Fixed)}
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
	// File is where the wizard keeps the setting it fixes, and StoreError
	// why it cannot write there.
	File       string `json:"database_file"`
	StoreError string `json:"store_error,omitempty"`
}

// Problem returns why DMRHub cannot reach its database.
func (f *DatabaseFix) Problem() Problem {
	p := Problem{File: f.State.Path, Locked: DatabaseLocked(f.Locks)}
	if f.Err != nil {
		p.Error = f.Err.Error()
	}
	if err := f.State.Writable(); err != nil {
		p.StoreError = err.Error()
	}
	return p
}

// Check returns why next cannot be tried: a change of a setting other than
// the database, or of a database setting that the deployment sets.
func (f *DatabaseFix) Check(next *config.Config) []config.ValidationError {
	var errs []config.ValidationError
	locked := DatabaseLocked(f.Locks)
	for _, k := range Keys() {
		if k.Equal(next, f.Base) {
			continue
		}
		switch {
		case !under(k.Path, "database"):
			errs = append(errs, config.ValidationError{Field: k.Path, Err: ErrNeedsDatabase})
		case locked:
			errs = append(errs, config.ValidationError{Field: k.Path, Err: ErrDatabaseLocked})
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return next.Database.ValidateWithFields()
}

// Fix connects to the database of next. When that works, it keeps next in
// the DatabaseState, hands the database to the start of DMRHub and returns
// the phase that comes next.
func (f *DatabaseFix) Fix(ctx context.Context, next config.Database) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return "", ErrDatabaseFixed
	}
	if DatabaseLocked(f.Locks) {
		return "", ErrDatabaseLocked
	}
	if err := f.State.Writable(); err != nil {
		return "", fmt.Errorf("the setup wizard cannot keep a database setting, so set the database in the config file or the environment of the deployment: %w", err)
	}
	cfg := *f.Base
	cfg.Database = next
	database, err := f.Open(&cfg)
	if err != nil {
		return "", fmt.Errorf("DMRHub cannot reach this database: %w", err)
	}
	if err := f.State.Save(next); err != nil {
		if sqlDB, derr := database.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return "", fmt.Errorf("DMRHub reached the database but cannot keep the setting, so set it in the config file or the environment of the deployment: %w", err)
	}
	f.done = true
	ack := make(chan string, 1)
	select {
	case f.fixed <- Fixed{DB: database, Config: &cfg, ack: ack}:
	case <-ctx.Done():
		return "", fmt.Errorf("the start of DMRHub did not take the database: %w", ctx.Err())
	}
	select {
	case phase := <-ack:
		return phase, nil
	case <-ctx.Done():
		return PhaseSetup, nil
	}
}
