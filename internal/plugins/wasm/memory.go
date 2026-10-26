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

package wasm

import (
	"sync/atomic"

	"github.com/tetratelabs/wazero/experimental"
)

// sliced is the memory of a plugin as a Go slice, which grows by a copy.
type sliced struct {
	buf []byte
}

func (s *sliced) Reallocate(size uint64) []byte {
	if size > uint64(cap(s.buf)) {
		grown := make([]byte, size)
		copy(grown, s.buf)
		s.buf = grown
	}
	s.buf = s.buf[:size]
	return s.buf
}

func (s *sliced) Free() {
	s.buf = nil
}

// limitedAllocator gives a plugin a memory that grows up to limit bytes,
// and notes in oom when the plugin asks for more. The runtime lets a
// memory grow to twice the limit, so that each grow that a plugin asks for
// reaches the allocator: the Go runtime grows its heap a few MiB at a
// time, TinyGo doubles it.
type limitedAllocator struct {
	limit uint64
	oom   *atomic.Bool
}

func (a limitedAllocator) Allocate(capBytes, maxBytes uint64) experimental.LinearMemory {
	most := max(min(maxBytes, a.limit), capBytes)
	if capBytes > a.limit {
		a.oom.Store(true)
	}
	return &limited{LinearMemory: newMemory(capBytes, most), most: most, oom: a.oom}
}

// limited is the memory of a plugin, which never grows past most bytes.
type limited struct {
	experimental.LinearMemory
	most uint64
	oom  *atomic.Bool
}

func (l *limited) Reallocate(size uint64) []byte {
	if size > l.most {
		l.oom.Store(true)
		return nil
	}
	return l.LinearMemory.Reallocate(size)
}
