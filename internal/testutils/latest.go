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

import "sync"

// Latest holds the last value that a polled condition saw, so that a test
// can report it after the poll. The Eventually of testify runs the
// condition on a goroutine of its own and does not wait for it after a
// timeout, so a condition must not write variables that the test reads.
// The zero Latest holds the zero value and is ready for use.
type Latest[T any] struct {
	mu sync.Mutex
	v  T
}

// Set makes v the latest value.
func (l *Latest[T]) Set(v T) {
	l.mu.Lock()
	l.v = v
	l.mu.Unlock()
}

// Get returns the latest value.
func (l *Latest[T]) Get() T {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.v
}
