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

// guests are the test plugins, built once with each compiler.
var guests struct { //nolint:gochecknoglobals // built once for all the tests
	once sync.Once
	dir  string
	err  error
}

// compilers are the compilers that the test plugins are built with: Go,
// and TinyGo, which builds the plugins that come with DMRHub. The tests
// run the tinygo command that the TINYGO environment variable names, or
// tinygo from the PATH.
var compilers = []string{"go", "tinygo"} //nolint:gochecknoglobals // a constant

func buildCommand(compiler, out, dir string) *exec.Cmd {
	if compiler == "go" {
		cmd := exec.Command("go", "build", "-o", out, dir) //nolint:noctx // a build for the tests
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		return cmd
	}
	tinygo := os.Getenv("TINYGO")
	if tinygo == "" {
		tinygo = "tinygo"
	}
	return exec.Command(tinygo, "build", "-target=wasip1", "-no-debug", "-o", out, dir) //nolint:noctx,gosec // a build for the tests
}

// guest returns the module of the test plugin in testdata/name, built for
// wasip1 with Go.
func guest(t testing.TB, name string) []byte {
	t.Helper()
	return guestOf(t, "go", name)
}

// guestOf returns the module of the test plugin in testdata/name, built for
// wasip1 with compiler.
func guestOf(t testing.TB, compiler, name string) []byte {
	t.Helper()
	guests.once.Do(func() {
		guests.dir, guests.err = os.MkdirTemp("", "dmrhub-wasm-guests-")
		if guests.err != nil {
			return
		}
		for _, c := range compilers {
			for _, g := range []string{"echo", "spin", "hog", "files", "netprobe", "silent", "deaf", "early", "panic"} {
				cmd := buildCommand(c, filepath.Join(guests.dir, c+"-"+g+".wasm"), "./testdata/"+g)
				if out, err := cmd.CombinedOutput(); err != nil {
					guests.err = fmt.Errorf("could not build %s with %s: %w\n%s", g, c, err, out)
					return
				}
			}
		}
	})
	if guests.err != nil {
		t.Fatal(guests.err)
	}
	b, err := os.ReadFile(filepath.Join(guests.dir, compiler+"-"+name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// eachCompiler runs test once for the test plugins of each compiler.
func eachCompiler(t *testing.T, test func(t *testing.T, compiler string)) {
	t.Helper()
	for _, c := range compilers {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			test(t, c)
		})
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if guests.dir != "" {
		_ = os.RemoveAll(guests.dir)
	}
	os.Exit(code)
}
