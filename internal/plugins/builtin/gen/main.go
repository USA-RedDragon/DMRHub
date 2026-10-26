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

// Command gen builds the plugins that come with DMRHub for wasip1 with
// TinyGo, as the one module of the guest command, and writes it, compressed
// with zstd with its size in the frame, next to the builtin package, which
// embeds it. go generate runs it. It runs the tinygo command that the
// TINYGO environment variable names, or tinygo from the PATH, at least
// version 0.43.0, the first whose goroutines run while a plugin waits for
// its hub.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func moduleRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("could not find the module: %w", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

// minTinyGo is the oldest TinyGo that builds the plugins: 0.43.0 parks a
// goroutine that reads stdin, so that the others and the timers run while
// the plugin waits for its hub.
var minTinyGo = [3]int{0, 43, 0} //nolint:gochecknoglobals // a constant

// errOldTinyGo is returned for a TinyGo older than minTinyGo.
var errOldTinyGo = errors.New("the built-in plugins need TinyGo 0.43.0 or later")

var tinygoVersion = regexp.MustCompile(`^tinygo version (\d+)\.(\d+)\.(\d+)`) //nolint:gochecknoglobals // a constant

// findTinyGo returns the tinygo command to build with, and checks its
// version.
func findTinyGo(ctx context.Context) (string, error) {
	tinygo := os.Getenv("TINYGO")
	if tinygo == "" {
		tinygo = "tinygo"
	}
	out, err := exec.CommandContext(ctx, tinygo, "version").Output() //nolint:gosec // the tinygo command
	if err != nil {
		return "", fmt.Errorf("could not run %s, which builds the built-in plugins: %w", tinygo, err)
	}
	if !versionAtLeast(string(out), minTinyGo) {
		return "", fmt.Errorf("%w: %s says %q", errOldTinyGo, tinygo, strings.TrimSpace(string(out)))
	}
	return tinygo, nil
}

// versionAtLeast reports whether the output of tinygo version says a
// version of at least least.
func versionAtLeast(version string, least [3]int) bool {
	m := tinygoVersion.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	var got [3]int
	for i := range got {
		got[i], _ = strconv.Atoi(m[i+1])
	}
	for i := range got {
		if got[i] != least[i] {
			return got[i] > least[i]
		}
	}
	return true
}

func build(ctx context.Context, root string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "dmrhub-builtin-")
	if err != nil {
		return nil, fmt.Errorf("could not make a build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tinygo, err := findTinyGo(ctx)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "builtin.wasm")
	cmd := exec.CommandContext(ctx, tinygo, "build", "-target=wasip1", "-no-debug", "-trimpath", "-o", out, "./internal/plugins/builtin/guest") //nolint:gosec // the tinygo command and the path of the guest
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not build the built-in plugins: %w", err)
	}
	b, err := os.ReadFile(out) //nolint:gosec // the file it just built
	if err != nil {
		return nil, fmt.Errorf("could not read the built-in plugins: %w", err)
	}
	return b, nil
}

func compress(b []byte) ([]byte, error) {
	w, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return nil, fmt.Errorf("could not compress: %w", err)
	}
	defer func() { _ = w.Close() }()
	return w.EncodeAll(b, nil), nil
}

func run(ctx context.Context) error {
	root, err := moduleRoot(ctx)
	if err != nil {
		return err
	}
	b, err := build(ctx, root)
	if err != nil {
		return err
	}
	z, err := compress(b)
	if err != nil {
		return err
	}
	if err := os.WriteFile("builtin.wasm.zst", z, 0o644); err != nil { //nolint:gosec // an asset of the build
		return fmt.Errorf("could not write the built-in plugins: %w", err)
	}
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
