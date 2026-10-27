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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	guests.once.Do(buildGuests)
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

// buildGuests builds every test plugin with each compiler, a few at a time.
// It keeps each module in the user cache under a hash of the compiler and
// of every file of the packages that the plugin is made of, so that a test
// run builds again only what changed.
func buildGuests() {
	guests.dir, guests.err = os.MkdirTemp("", "dmrhub-wasm-guests-")
	if guests.err != nil {
		return
	}
	cache := ""
	if dir, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(dir, "dmrhub-test-guests")
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, c := range compilers {
		for _, g := range []string{"echo", "spin", "hog", "files", "netprobe", "silent", "deaf", "early", "panic"} {
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				if err := buildGuest(cache, c, g); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	guests.err = errors.Join(errs...)
}

func buildGuest(cache, compiler, name string) error {
	out := filepath.Join(guests.dir, compiler+"-"+name+".wasm")
	dir := "./testdata/" + name
	key, err := guestKey(compiler, dir)
	if err != nil || cache == "" {
		return build(compiler, out, dir, name)
	}
	cached := filepath.Join(cache, key+".wasm")
	if b, err := os.ReadFile(cached); err == nil {
		if err := os.WriteFile(out, b, 0o600); err != nil {
			return fmt.Errorf("could not write %s: %w", out, err)
		}
		return nil
	}
	if err := build(compiler, out, dir, name); err != nil {
		return err
	}
	keep(cache, cached, out)
	return nil
}

// keep copies the module at out to cached, when it can, for later runs.
func keep(cache, cached, out string) {
	b, err := os.ReadFile(out)
	if err != nil || os.MkdirAll(cache, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(cache, filepath.Base(cached)+"-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	if cerr := tmp.Close(); werr == nil && cerr == nil {
		_ = os.Rename(tmp.Name(), cached)
	}
	_ = os.Remove(tmp.Name())
}

func build(compiler, out, dir, name string) error {
	cmd := buildCommand(compiler, out, dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("could not build %s with %s: %w\n%s", name, compiler, err, b)
	}
	return nil
}

// guestKey hashes what the module of the test plugin in dir is built from:
// the version of the compiler, the build flags, go.mod, and every file of every
// package outside the standard library that the plugin imports.
func guestKey(compiler, dir string) (string, error) {
	h := sha256.New()
	tool := compiler
	if compiler == "tinygo" {
		tool = buildCommand(compiler, "", dir).Path
	}
	for _, args := range [][]string{{"go", "version"}, {tool, "version"}} {
		v, err := exec.Command(args[0], args[1:]...).Output() //nolint:noctx,gosec // a version for the tests
		if err != nil {
			return "", fmt.Errorf("could not get the version of %s: %w", args[0], err)
		}
		_, _ = fmt.Fprintf(h, "%s\x00", v)
	}
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00", compiler, os.Getenv("GOFLAGS"))
	gomod, err := exec.Command("go", "env", "GOMOD").Output() //nolint:noctx // a path for the tests
	if err != nil {
		return "", fmt.Errorf("could not find go.mod: %w", err)
	}
	mod, err := os.ReadFile(strings.TrimSpace(string(gomod)))
	if err != nil {
		return "", fmt.Errorf("could not read go.mod: %w", err)
	}
	h.Write(mod)
	list := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", dir) //nolint:noctx // a listing for the tests
	list.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	dirs, err := list.Output()
	if err != nil {
		return "", fmt.Errorf("could not list the packages of %s: %w", dir, err)
	}
	for _, d := range strings.Fields(string(dirs)) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return "", fmt.Errorf("could not read %s: %w", d, err)
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(d, e.Name()))
			if err != nil {
				return "", fmt.Errorf("could not read %s: %w", e.Name(), err)
			}
			_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00", d, e.Name(), len(b))
			h.Write(b)
		}
	}
	return compiler + "-" + filepath.Base(dir) + "-" + hex.EncodeToString(h.Sum(nil)), nil
}

// TestMain builds the test plugins before any test starts, so that no
// test waits for the builds with its clocks running and its in-memory
// database open, which closes when its idle connections expire.
func TestMain(m *testing.M) {
	guests.once.Do(buildGuests)
	code := m.Run()
	if guests.dir != "" {
		_ = os.RemoveAll(guests.dir)
	}
	os.Exit(code)
}
