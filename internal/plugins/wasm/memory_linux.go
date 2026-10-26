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

//go:build linux

package wasm

import (
	"syscall"

	"github.com/tetratelabs/wazero/experimental"
)

// mapped is the memory of a plugin, mapped from the system for its most
// pages, outside the Go heap. The system gives a page only when the plugin
// first writes to it, so a plugin holds only the memory it used, and its
// memory never moves or is copied when it grows.
type mapped struct {
	buf []byte
}

func (m *mapped) Reallocate(size uint64) []byte {
	if m.buf == nil || size > uint64(len(m.buf)) {
		return nil
	}
	return m.buf[:size:size]
}

func (m *mapped) Free() {
	if m.buf != nil {
		_ = syscall.Munmap(m.buf)
		m.buf = nil
	}
}

// mapMemory returns a memory of at most maxBytes that the system maps.
// When the system cannot map it, the memory is a Go slice.
func mapMemory(_, maxBytes uint64) experimental.LinearMemory {
	if maxBytes == 0 || maxBytes > 1<<32 {
		return &sliced{}
	}
	buf, err := syscall.Mmap(-1, 0, int(maxBytes), syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS|syscall.MAP_NORESERVE)
	if err != nil {
		return &sliced{}
	}
	return &mapped{buf: buf}
}

func newMemory(capBytes, maxBytes uint64) experimental.LinearMemory {
	return mapMemory(capBytes, maxBytes)
}
