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

package setupwizard

import (
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"gorm.io/gorm"
)

// State is the phase of the setup wizard: while DMRHub cannot reach its
// database, a DatabaseFix; after, the Live and the database of DMRHub.
type State struct {
	mu   sync.RWMutex
	fix  *liveconfig.DatabaseFix
	live *liveconfig.Live
	db   *gorm.DB
}

// NewState returns a State with neither a database fix nor a Live.
func NewState() *State {
	return &State{}
}

// SetDatabaseFix starts the database phase.
func (s *State) SetDatabaseFix(f *liveconfig.DatabaseFix) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fix, s.live, s.db = f, nil, nil
}

// SetLive starts the setup phase, with the Live and the database of
// DMRHub.
func (s *State) SetLive(l *liveconfig.Live, db *gorm.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fix, s.live, s.db = nil, l, db
}

// Fix returns the database fix of the database phase, or nil.
func (s *State) Fix() *liveconfig.DatabaseFix {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fix
}

// Live returns the Live of the setup phase, or nil.
func (s *State) Live() *liveconfig.Live {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

// DB returns the database of the setup phase, or nil.
func (s *State) DB() *gorm.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}
