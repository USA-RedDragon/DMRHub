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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func sh(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

func release(t *testing.T, repo, releases, tag string, now string, names ...string) {
	t.Helper()
	assets := filepath.Join(repo, "internal/http/frontend/dist/assets")
	if err := os.RemoveAll(filepath.Join(repo, "internal/http/frontend/dist")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(assets, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sh(t, repo, []string{"CARRY_REPO=file://" + releases, "CARRY_NOW=" + now}, "sh", "hack/carry/frontend.sh")
	dest := filepath.Join(releases, "releases/download", tag)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo, "artifacts/frontend-assets.tar.zst"), filepath.Join(dest, "frontend-assets.tar.zst")); err != nil {
		t.Fatal(err)
	}
}

func TestReleasesCarryTheAssetsOfTheReleaseBefore(t *testing.T) {
	t.Parallel()
	repo, releases := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		sh(t, repo, []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org"}, "git", args...)
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("internal/http/frontend/dist\nartifacts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitignore")
	git("commit", "-q", "-m", "old")
	git("tag", "v1.0.0")
	if err := os.MkdirAll(filepath.Join(repo, "hack/carry"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"carry-assets.sh", "frontend.sh"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "hack/carry", f), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "hack")
	git("commit", "-q", "-m", "carry")
	git("tag", "v1.1.0")
	release(t, repo, releases, "v1.1.0", "1000", "index-a.js", "shared.css")

	git("commit", "-q", "--allow-empty", "-m", "next")
	git("tag", "v1.2.0")
	release(t, repo, releases, "v1.2.0", "2000", "index-b.js", "shared.css")

	got, err := filepath.Glob(filepath.Join(repo, "internal/http/frontend/dist/assets/*"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i] = filepath.Base(got[i])
	}
	if want := []string{"index-a.js", "index-b.js", "shared.css"}; !slices.Equal(got, want) {
		t.Fatalf("the v1.2.0 build has %v, want %v", got, want)
	}

	if err := os.Remove(filepath.Join(releases, "releases/download/v1.2.0/frontend-assets.tar.zst")); err != nil {
		t.Fatal(err)
	}
	git("commit", "-q", "--allow-empty", "-m", "after")
	git("tag", "v1.3.0")
	cmd := exec.CommandContext(t.Context(), "sh", "hack/carry/frontend.sh")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "CARRY_REPO=file://"+releases)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Errorf("a release whose previous release has no carried assets passed: %s", out)
	}
}
