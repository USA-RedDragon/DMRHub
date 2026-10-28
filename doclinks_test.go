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
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const repoBlobPrefix = "https://github.com/USA-RedDragon/DMRHub/blob/main/"

func docSkipDir(name string) bool {
	switch name {
	case ".git", ".claude", "node_modules", "dist", "bin":
		return true
	}
	return false
}

func headingSlug(text string) string {
	text = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(text, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func markdownAnchors(text string) map[string]bool {
	anchors := map[string]bool{}
	seen := map[string]int{}
	fence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		for _, m := range regexp.MustCompile(`<a\s+(?:id|name)="([^"]+)"`).FindAllStringSubmatch(line, -1) {
			anchors[m[1]] = true
		}
		if !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if heading == "" || len(heading) == len(line) {
			continue
		}
		slug := headingSlug(heading)
		if n := seen[slug]; n > 0 {
			anchors[slug+"-"+strconv.Itoa(n)] = true
		} else {
			anchors[slug] = true
		}
		seen[slug]++
	}
	return anchors
}

func markdownTargets(text string) []string {
	var targets []string
	fence := false
	link := regexp.MustCompile(`\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	href := regexp.MustCompile(`(?:href|src)="([^"]+)"`)
	code := regexp.MustCompile("`[^`]*`")
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		line = code.ReplaceAllString(line, "")
		for _, m := range link.FindAllStringSubmatch(line, -1) {
			targets = append(targets, m[1])
		}
		for _, m := range href.FindAllStringSubmatch(line, -1) {
			targets = append(targets, m[1])
		}
	}
	return targets
}

type docLinkChecker struct {
	t       *testing.T
	anchors map[string]map[string]bool
}

func (c *docLinkChecker) anchorsOf(file string) map[string]bool {
	if a, ok := c.anchors[file]; ok {
		return a
	}
	data, err := os.ReadFile(file)
	if err != nil {
		c.t.Fatalf("read %s: %v", file, err)
	}
	a := markdownAnchors(string(data))
	c.anchors[file] = a
	return a
}

func (c *docLinkChecker) check(from, target, resolved string) {
	file, frag, _ := strings.Cut(resolved, "#")
	info, err := os.Stat(filepath.FromSlash(file))
	if err != nil {
		c.t.Errorf("%s: link %q: %s does not exist", from, target, file)
		return
	}
	if frag == "" || info.IsDir() || !strings.HasSuffix(file, ".md") {
		return
	}
	if !c.anchorsOf(filepath.FromSlash(file))[frag] {
		c.t.Errorf("%s: link %q: %s has no heading for #%s", from, target, file, frag)
	}
}

func (c *docLinkChecker) checkTarget(from, target string) {
	switch {
	case strings.HasPrefix(target, repoBlobPrefix), strings.HasPrefix(target, "https://github.com/USA-RedDragon/DMRHub/tree/main/"):
		rest := strings.TrimPrefix(strings.TrimPrefix(target, repoBlobPrefix), "https://github.com/USA-RedDragon/DMRHub/tree/main/")
		c.check(from, target, rest)
	case regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`).MatchString(target), strings.HasPrefix(target, "//"), strings.Contains(target, "<"):
	case strings.HasPrefix(target, "#"):
		c.check(from, target, filepath.ToSlash(from)+target)
	case strings.HasPrefix(target, "/"):
	default:
		c.check(from, target, path.Join(path.Dir(filepath.ToSlash(from)), target))
	}
}

// TestDocLinks checks that every relative link in the Markdown files, and
// every link to a file of this repository in the docs and the frontend, names
// a file that exists and a heading that the file has.
func TestDocLinks(t *testing.T) {
	t.Parallel()
	c := &docLinkChecker{t: t, anchors: map[string]map[string]bool{}}
	repoLink := regexp.MustCompile(regexp.QuoteMeta(repoBlobPrefix) + `[^"'\s)<>]+`)
	files := 0
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && docSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".md":
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read %s: %w", p, err)
			}
			files++
			for _, target := range markdownTargets(string(data)) {
				c.checkTarget(p, target)
			}
		case ".vue", ".ts":
			if !strings.HasPrefix(filepath.ToSlash(p), "internal/http/frontend/src/") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read %s: %w", p, err)
			}
			for _, target := range repoLink.FindAllString(string(data), -1) {
				c.checkTarget(p, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 10 {
		t.Fatalf("found only %d Markdown files", files)
	}
}

func TestHeadingSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Run plugins":                    "run-plugins",
		"Use `dmrhub-parrot` (optional)": "use-dmrhub-parrot-optional",
		"Upgrade from 1.x: what changed": "upgrade-from-1x-what-changed",
		"See [Positions](positions.md)":  "see-positions",
	} {
		if got := headingSlug(in); got != want {
			t.Errorf("headingSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
