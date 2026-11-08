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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// isGlobalSetter reports whether pkg.name is a call that changes a logger
// of the whole process.
func isGlobalSetter(pkg, name string) bool {
	switch pkg + "." + name {
	case "slog.SetDefault", "slog.SetLogLoggerLevel", "log.SetOutput", "log.SetFlags", "log.SetPrefix", "log.SetDefault":
		return true
	}
	return false
}

func callsSetter(n ast.Node, swappers map[string]bool) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		switch f := call.Fun.(type) {
		case *ast.SelectorExpr:
			if x, ok := f.X.(*ast.Ident); ok && isGlobalSetter(x.Name, f.Sel.Name) {
				found = true
			}
			if swappers[f.Sel.Name] {
				found = true
			}
		case *ast.Ident:
			if swappers[f.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

// callsParallel reports whether the body of fn calls t.Parallel on its own
// *testing.T, outside the funcs that it runs as subtests.
func callsParallel(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 || len(fn.Type.Params.List[0].Names) == 0 {
		return false
	}
	param := fn.Type.Params.List[0].Names[0].Name
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok || found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if f, ok := call.Fun.(*ast.SelectorExpr); ok && f.Sel.Name == "Parallel" {
			if x, ok := f.X.(*ast.Ident); ok && x.Name == param {
				found = true
			}
		}
		return !found
	})
	return found
}

// parallelSwaps returns the names of the tests in files, the test files of
// one directory, that call t.Parallel and change a process-wide logger,
// themselves or through a func of the directory.
func parallelSwaps(files []*ast.File) []string {
	var funcs []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				funcs = append(funcs, fn)
			}
		}
	}
	swappers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, fn := range funcs {
			if !swappers[fn.Name.Name] && callsSetter(fn.Body, swappers) {
				swappers[fn.Name.Name] = true
				changed = true
			}
		}
	}
	var out []string
	for _, fn := range funcs {
		if fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && swappers[fn.Name.Name] && callsParallel(fn) {
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

func parseSources(t *testing.T, srcs map[string]string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(srcs))
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		files = append(files, f)
	}
	return files
}

// TestAParallelTestThatSwapsTheLoggerIsFound checks parallelSwaps on a
// test that swaps the default logger itself, one that swaps it through a
// helper of another file, one that does not run in parallel and one whose
// subtests alone run in parallel.
func TestAParallelTestThatSwapsTheLoggerIsFound(t *testing.T) {
	t.Parallel()
	files := parseSources(t, map[string]string{
		"a_test.go": `package x_test

func TestDirect(t *testing.T) {
	t.Parallel()
	slog.SetDefault(slog.New(h))
}

func TestHelper(t *testing.T) {
	t.Parallel()
	_ = captured(t)
}

func TestAlone(t *testing.T) {
	slog.SetDefault(slog.New(h))
	t.Run("a", func(t *testing.T) { t.Parallel() })
}
`,
		"b_test.go": `package x_test

func captured(t *testing.T) *bytes.Buffer {
	return capture(t)
}

func capture(t *testing.T) *bytes.Buffer {
	log.SetOutput(b)
	return b
}
`,
	})
	got := parallelSwaps(files)
	sort.Strings(got)
	require.Equal(t, []string{"TestDirect", "TestHelper"}, got)
}

// TestNoParallelTestSwapsAGlobalLogger fails on each test that calls
// t.Parallel and changes the default slog or log logger, itself or through
// a helper of its package: the other parallel tests of the process would
// log to its logger while it runs, so their logs would be lost and could
// fail its checks. Such a test must run alone, without t.Parallel.
func TestNoParallelTestSwapsAGlobalLogger(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	byDir := map[string][]*ast.File{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "frontend", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		f, err := parser.ParseFile(fset, path, b, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		byDir[filepath.Dir(path)] = append(byDir[filepath.Dir(path)], f)
		return nil
	})
	require.NoError(t, err)
	for dir, files := range byDir {
		rel, _ := filepath.Rel(root, dir)
		for _, name := range parallelSwaps(files) {
			t.Errorf("%s: %s calls t.Parallel and changes a process-wide logger; run it without t.Parallel", filepath.ToSlash(rel), name)
		}
	}
}
