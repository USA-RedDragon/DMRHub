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

package config

import "sync/atomic"

// Source gives the config that DMRHub runs with now.
type Source interface {
	// Get returns the config. The caller must not change it.
	Get() *Config
}

// Get returns a copy of c, so that a config is a Source of its values at
// each call.
func (c Config) Get() *Config {
	return &c
}

// Current holds the config that DMRHub runs with. Setup replaces it while
// DMRHub runs, so a reader takes it again with Get for each use. A config
// that Current holds never changes; Set stores a new one.
type Current struct {
	p atomic.Pointer[Config]
}

// NewCurrent returns a Current that holds c.
func NewCurrent(c *Config) *Current {
	cur := &Current{}
	cur.p.Store(c)
	return cur
}

// Get returns the config that DMRHub runs with now.
func (c *Current) Get() *Config {
	return c.p.Load()
}

// Set makes next the config that DMRHub runs with. The caller must not
// change next after the call.
func (c *Current) Set(next *Config) {
	c.p.Store(next)
}
