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

// Command gen builds the plugins that come with DMRHub for wasip1, and
// writes each, compressed with zstd, next to the builtin package, which
// embeds them. go generate runs it.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func build(ctx context.Context, root, name string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "dmrhub-builtin-")
	if err != nil {
		return nil, fmt.Errorf("could not make a build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, name+".wasm")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./plugins/"+name+"/cmd/"+name) //nolint:gosec // the names of the built-in plugins
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not build %s: %w", name, err)
	}
	b, err := os.ReadFile(out) //nolint:gosec // the file it just built
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", name, err)
	}
	return b, nil
}

func compress(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return nil, fmt.Errorf("could not compress: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return nil, fmt.Errorf("could not compress: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("could not compress: %w", err)
	}
	return buf.Bytes(), nil
}

func run(ctx context.Context) error {
	root, err := moduleRoot(ctx)
	if err != nil {
		return err
	}
	for _, name := range []string{"parrot", "locator"} {
		b, err := build(ctx, root, name)
		if err != nil {
			return err
		}
		z, err := compress(b)
		if err != nil {
			return err
		}
		if err := os.WriteFile(name+".wasm.zst", z, 0o644); err != nil { //nolint:gosec // an asset of the build
			return fmt.Errorf("could not write %s: %w", name, err)
		}
	}
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
