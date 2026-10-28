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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const clockEnv = "DMRHUB_SHOTS_CLOCK"

const clockSlack = 500 * time.Millisecond

const clockAnchor = "\tsec, nsec, mono := runtimeNow()\n\tif mono == 0 {"

const clockPatch = "\tsec, nsec, mono := runtimeNow()\n" +
	"\tif off, ok := shotsOffset(); ok {\n" +
	"\t\tv := mono + off\n" +
	"\t\tif off&(1<<62) != 0 {\n" +
	"\t\t\tv = off &^ (1 << 62)\n" +
	"\t\t}\n" +
	"\t\treturn Time{uint64(v % 1e9), v/1e9 + unixToInternal, Local}\n" +
	"\t}\n" +
	"\tif mono == 0 {"

const clockFile = `package time

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var shotsOnce sync.Once
var shotsMem []byte

func shotsOffset() (int64, bool) {
	shotsOnce.Do(func() {
		path, ok := syscall.Getenv("` + clockEnv + `")
		if !ok {
			return
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
		if err != nil {
			return
		}
		m, err := syscall.Mmap(fd, 0, 8, syscall.PROT_READ, syscall.MAP_SHARED)
		_ = syscall.Close(fd)
		if err == nil {
			shotsMem = m
		}
	})
	if shotsMem == nil {
		return 0, false
	}
	return atomic.LoadInt64((*int64)(unsafe.Pointer(&shotsMem[0]))), true
}
`

const frozenBit = int64(1) << 62

func writeOverlay(goroot, dir string) error {
	src := filepath.Join(goroot, "src", "time", "time.go")
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", src, err)
	}
	if strings.Count(string(data), clockAnchor) != 1 {
		return errors.New("time.Now of this Go release does not start as the clock patch expects")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("could not make %s: %w", dir, err)
	}
	patched := filepath.Join(dir, "time.go")
	extra := filepath.Join(dir, "zz_shotsclock.go")
	if err := os.WriteFile(patched, []byte(strings.Replace(string(data), clockAnchor, clockPatch, 1)), 0o600); err != nil {
		return fmt.Errorf("could not write the clock patch: %w", err)
	}
	if err := os.WriteFile(extra, []byte(clockFile), 0o600); err != nil {
		return fmt.Errorf("could not write the clock patch: %w", err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {
		src: patched,
		filepath.Join(goroot, "src", "time", "zz_shotsclock.go"): extra,
	}})
	if err != nil {
		return fmt.Errorf("could not encode the overlay: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "overlay.json"), overlay, 0o600); err != nil {
		return fmt.Errorf("could not write the overlay: %w", err)
	}
	return nil
}

type clock struct {
	path string
	mem  []byte
}

func openClock(path string) (*clock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("could not open the clock: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(8); err != nil {
		return nil, fmt.Errorf("could not size the clock: %w", err)
	}
	mem, err := unix.Mmap(int(f.Fd()), 0, 8, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("could not map the clock: %w", err)
	}
	return &clock{path: path, mem: mem}, nil
}

func monotonic() int64 {
	var ts unix.Timespec
	_ = unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return ts.Nano()
}

func (c *clock) word() *int64 {
	return (*int64)(unsafe.Pointer(&c.mem[0]))
}

func (c *clock) now() time.Time {
	w := atomic.LoadInt64(c.word())
	if w&frozenBit != 0 {
		return time.Unix(0, w&^frozenBit)
	}
	return time.Unix(0, monotonic()+w)
}

func (c *clock) frozen() bool {
	return atomic.LoadInt64(c.word())&frozenBit != 0
}

func (c *clock) freeze(t time.Time) {
	atomic.StoreInt64(c.word(), t.UnixNano()|frozenBit)
}

func (c *clock) set(t time.Time) error {
	if now := c.now(); atomic.LoadInt64(c.word()) != 0 && now.Sub(t) > clockSlack {
		return fmt.Errorf("the demo clock is at %s, after the next step at %s: a step took longer than the time the shot list gives it", now.Format(time.RFC3339Nano), t.Format(time.RFC3339))
	}
	atomic.StoreInt64(c.word(), t.UnixNano()-monotonic())
	return nil
}

func (c *clock) close() {
	_ = unix.Munmap(c.mem)
}
