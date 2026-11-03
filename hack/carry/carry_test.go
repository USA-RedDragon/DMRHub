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

package carry

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const day = 86400

type image struct {
	root string
}

func (i image) ledger() string { return filepath.Join(i.root, "ledger") }
func (i image) dir() string    { return filepath.Join(i.root, "assets") }

func (i image) files(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(i.dir())
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func run(t *testing.T, prev image, now int64, env []string, names ...string) (image, error) {
	t.Helper()
	next := image{root: t.TempDir()}
	if err := os.MkdirAll(next.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(next.dir(), n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "sh", "carry-assets.sh", prev.ledger(), next.ledger(), prev.dir(), next.dir())
	cmd.Env = append(os.Environ(), append(env, "CARRY_NOW="+strconv.FormatInt(now, 10))...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return next, fmt.Errorf("%w: %s", err, out)
	}
	return next, nil
}

func build(t *testing.T, prev image, now int64, names ...string) image {
	t.Helper()
	next, err := run(t, prev, now, nil, names...)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func first(t *testing.T, now int64, names ...string) image {
	t.Helper()
	next, err := run(t, image{root: t.TempDir()}, now, []string{"CARRY_FIRST_BUILD=1"}, names...)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestCarriesTheFilesTheNewBuildLacks(t *testing.T) {
	t.Parallel()
	a := first(t, 10*day, "shared.css", "a.js")
	b := build(t, a, 10*day+60, "shared.css", "b.js")
	if got, want := b.files(t), []string{"a.js", "b.js", "shared.css"}; !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	info, err := os.Stat(filepath.Join(b.dir(), "shared.css"))
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Unix() != 10*day+60 {
		t.Errorf("a file of the new build has time %d, want the build time", info.ModTime().Unix())
	}
}

func TestKeepsTheLastBuildsWhenBuildsAreRare(t *testing.T) {
	t.Parallel()
	img := first(t, 0, "b0.js")
	for i := int64(1); i <= 5; i++ {
		img = build(t, img, i*30*day, fmt.Sprintf("b%d.js", i))
	}
	if got, want := img.files(t), []string{"b2.js", "b3.js", "b4.js", "b5.js"}; !slices.Equal(got, want) {
		t.Fatalf("files %v, want the new build and the 3 before it %v", got, want)
	}
}

func TestKeepsTheLastDaysWhenBuildsAreFrequent(t *testing.T) {
	t.Parallel()
	img := first(t, 0, "b0.js")
	for i := int64(1); i <= 20; i++ {
		img = build(t, img, i*day, fmt.Sprintf("b%d.js", i))
	}
	var want []string
	for i := 13; i <= 20; i++ {
		want = append(want, fmt.Sprintf("b%d.js", i))
	}
	slices.Sort(want)
	if got := img.files(t); !slices.Equal(got, want) {
		t.Fatalf("files %v, want the builds of the last 7 days %v", got, want)
	}
	ledger, err := os.ReadFile(img.ledger())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(string(ledger))); n != 8 {
		t.Errorf("the ledger lists %d builds, want 8", n)
	}
}

func TestCarriesAllFilesOfAnImageWithoutALedger(t *testing.T) {
	t.Parallel()
	prev := image{root: t.TempDir()}
	if err := os.MkdirAll(prev.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prev.dir(), "old.js"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	next := build(t, prev, 100*day, "new.js")
	if got, want := next.files(t), []string{"new.js", "old.js"}; !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
}

func TestFailsWithoutThePreviousAssets(t *testing.T) {
	t.Parallel()
	if _, err := run(t, image{root: t.TempDir()}, day, nil, "a.js"); err == nil {
		t.Error("a build with no previous assets and no CARRY_FIRST_BUILD passed")
	}
	a := first(t, day, "a.js")
	if err := os.RemoveAll(a.dir()); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, a, 2*day, nil, "b.js"); err == nil {
		t.Error("a build whose previous image has a ledger but no assets passed")
	}
}
