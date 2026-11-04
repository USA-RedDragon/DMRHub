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

package servers

import "sync"

// Tracked runs functions on goroutines that Wait waits for. Once Wait has
// begun, Go runs the function on the goroutine that calls Go instead, so
// that no goroutine starts that Wait does not wait for. The zero value is
// ready to use.
type Tracked struct {
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// Go runs fn on a new goroutine, or on this one once Wait has begun.
func (t *Tracked) Go(fn func()) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		fn()
		return
	}
	t.wg.Add(1)
	t.mu.Unlock()
	go func() {
		defer t.wg.Done()
		fn()
	}()
}

// Wait waits for the goroutines of Go to end.
func (t *Tracked) Wait() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.wg.Wait()
}
