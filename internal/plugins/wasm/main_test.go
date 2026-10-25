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

package wasm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// guests are the test plugins, built once.
var guests struct { //nolint:gochecknoglobals // built once for all the tests
	once sync.Once
	dir  string
	err  error
}

// guest returns the module of the test plugin in testdata/name, built for
// wasip1.
func guest(t testing.TB, name string) []byte {
	t.Helper()
	guests.once.Do(func() {
		guests.dir, guests.err = os.MkdirTemp("", "dmrhub-wasm-guests-")
		if guests.err != nil {
			return
		}
		for _, g := range []string{"echo", "spin", "hog", "files", "netprobe", "silent"} {
			cmd := exec.Command("go", "build", "-o", filepath.Join(guests.dir, g+".wasm"), "./testdata/"+g) //nolint:noctx // a build for the tests
			cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
			if out, err := cmd.CombinedOutput(); err != nil {
				guests.err = fmt.Errorf("could not build %s: %w\n%s", g, err, out)
				return
			}
		}
	})
	if guests.err != nil {
		t.Fatal(guests.err)
	}
	b, err := os.ReadFile(filepath.Join(guests.dir, name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMain(m *testing.M) {
	code := m.Run()
	if guests.dir != "" {
		_ = os.RemoveAll(guests.dir)
	}
	os.Exit(code)
}
