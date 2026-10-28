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

package testtiming_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const allowedFile = "testdata/allowed.txt"

type class struct {
	name string
	re   *regexp.Regexp
}

func classes() []class {
	return []class{
		{"sleep", regexp.MustCompile(`\btime\.Sleep\(`)},
		{"eventually", regexp.MustCompile(`\b(require|assert)\.(Eventually|EventuallyWithT|Never)\(`)},
		{"elapsed", regexp.MustCompile(`(time\.Since\(|time\.Until\(|\.Sub\()`)},
		{"timer", regexp.MustCompile(`(time\.After\(|time\.NewTimer\(|time\.NewTicker\(|time\.Tick\()`)},
		{"fixedport", regexp.MustCompile(`(127\.0\.0\.1:\d{2,5}|localhost:\d{2,5}|"\:\d{3,5}"|Port\s*[:=]\s*\d{3,5}\b)`)},
		{"pickport", regexp.MustCompile(`\b(FreeUDPPort|FreeTCPPort|freePort)\(`)},
	}
}

type site struct{ class, file string }

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod above the test")
		dir = parent
	}
}

func scan(t *testing.T, root string) map[site]int {
	t.Helper()
	found := map[site]int{}
	cs := classes()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "frontend", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.Contains(d.Name(), "_gen") || strings.HasPrefix(rel, "internal/testtiming/") || (!strings.HasSuffix(rel, "_test.go") && !strings.HasPrefix(rel, "internal/testutils/")) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		counts, err := scanFile(cs, rel, b)
		if err != nil {
			return err
		}
		for class, n := range counts {
			found[site{class, rel}] += n
		}
		return nil
	})
	require.NoError(t, err)
	return found
}

// scanFile counts the sites of each class in the Go source b, but not
// those in the func that a synctest.Test call runs: in its bubble the time
// is virtual, so a sleep or a timeout there does not depend on the load of
// the machine.
func scanFile(cs []class, name string, b []byte) (map[string]int, error) {
	bubbled, err := bubbleLines(name, b)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for i, line := range strings.Split(string(b), "\n") {
		if bubbled[i+1] || strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, c := range cs {
			if c.re.MatchString(line) {
				counts[c.name]++
			}
		}
	}
	return counts, nil
}

// bubbleLines returns the lines of the bodies of the funcs that the
// synctest.Test calls in b run.
func bubbleLines(name string, b []byte) (map[int]bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, b, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	lines := map[int]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Test" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "synctest" {
			return true
		}
		lit, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}
		for l := fset.Position(lit.Body.Lbrace).Line; l <= fset.Position(lit.Body.Rbrace).Line; l++ {
			lines[l] = true
		}
		return true
	})
	return lines, nil
}

type allowance struct {
	count  int
	reason string
}

func readAllowed(t *testing.T) map[site]allowance {
	t.Helper()
	f, err := os.Open(allowedFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	out := map[site]allowance{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "\t", 4)
		require.Len(t, f, 4, "%s:%d is not class, file, count and reason, tab separated", allowedFile, n)
		count, err := strconv.Atoi(f[2])
		require.NoError(t, err, "%s:%d", allowedFile, n)
		require.NotEmpty(t, strings.TrimSpace(f[3]), "%s:%d gives no reason", allowedFile, n)
		out[site{f[0], f[1]}] = allowance{count, f[3]}
	}
	require.NoError(t, sc.Err())
	return out
}

func writeAllowed(t *testing.T, found map[site]int, old map[site]allowance) {
	t.Helper()
	keys := make([]site, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].file != keys[j].file {
			return keys[i].file < keys[j].file
		}
		return keys[i].class < keys[j].class
	})
	var b strings.Builder
	b.WriteString("# class\tfile\tcount\treason. Generated by DMRHUB_TIMING_ALLOWED=write go test ./internal/testtiming; counts only go down.\n")
	for _, k := range keys {
		reason := "baseline of 2026-10-28, not reviewed: replace with an event, a test clock or a :0 port"
		if a, ok := old[k]; ok {
			reason = a.reason
		}
		fmt.Fprintf(&b, "%s\t%s\t%d\t%s\n", k.class, k.file, found[k], reason)
	}
	require.NoError(t, os.WriteFile(allowedFile, []byte(b.String()), 0o600))
}

// TestASiteInABubbleIsNotCounted counts the sites of a source with a sleep
// in a synctest bubble and one outside it.
func TestASiteInABubbleIsNotCounted(t *testing.T) {
	t.Parallel()
	src := []byte(`package x_test

func TestX(t *testing.T) {
	time.Sleep(time.Second)
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Second)
		<-time.After(time.Second)
	})
}
`)
	counts, err := scanFile(classes(), "x_test.go", src)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"sleep": 1}, counts)
}

// TestNoNewWallClockWaits fails on each site in a test, or in
// internal/testutils, that sleeps, bounds a wait with a wall-clock timeout,
// measures elapsed time, starts a timer or names a port, past the count
// that testdata/allowed.txt allows for its class and file. It also fails
// when a file has fewer such sites than allowed, so that the list shrinks
// with each fix. DMRHUB_TIMING_ALLOWED=write writes the list from the tree,
// keeping the reasons of the entries it had.
func TestNoNewWallClockWaits(t *testing.T) {
	t.Parallel()
	found := scan(t, moduleRoot(t))
	allowed := readAllowed(t)
	if os.Getenv("DMRHUB_TIMING_ALLOWED") == "write" {
		writeAllowed(t, found, allowed)
		return
	}
	for k, n := range found {
		a := allowed[k]
		if n > a.count {
			t.Errorf("%s has %d %s sites, %d allowed: wait on an event, use a test clock or bind port 0 instead", k.file, n, k.class, a.count)
		}
	}
	for k, a := range allowed {
		if n := found[k]; n < a.count {
			t.Errorf("%s has %d %s sites, fewer than the %d allowed: lower its count in %s", k.file, n, k.class, a.count, allowedFile)
		}
	}
}
