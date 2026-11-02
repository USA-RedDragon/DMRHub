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

package hub_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheHubStartsGoroutinesOnlyThroughGoTracked parses the package and
// fails on each go statement outside goTracked, whose goroutines Stop
// waits for. A goroutine that Stop does not wait for can use the
// database or the pubsub after the caller closed them.
func TestTheHubStartsGoroutinesOnlyThroughGoTracked(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	var bare []string
	tracked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			ast.Inspect(decl, func(n ast.Node) bool {
				if _, ok := n.(*ast.GoStmt); !ok {
					return true
				}
				if isFunc && fn.Name.Name == "goTracked" && fn.Recv != nil {
					tracked++
					return true
				}
				bare = append(bare, fset.Position(n.Pos()).String())
				return true
			})
		}
	}
	require.Empty(t, bare, "go statements outside goTracked")
	require.Equal(t, 1, tracked, "goTracked starts the goroutines")
}

// TestTheHubReachesItsStoresOnlyThroughItsStopContext parses the package
// and fails on each way to reach the database or the KV store of a hub
// other than through h.db and h.kv, which NewHub makes with stoppingDB and
// stoppingStore so that Stop ends their calls: a new context on a gorm
// call, an assignment of a db or kv field, or a use of the database or KV
// store that NewHub gets other than in those wrappers, in the event log
// and in the transcoder, which end their own calls when the hub closes
// them.
func TestTheHubReachesItsStoresOnlyThroughItsStopContext(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	var bad []string
	allowed := map[string]bool{"stoppingDB": true, "stoppingStore": true, "connlog.New": true, "transcode.NewService": true}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			var stack []ast.Node
			ast.Inspect(decl, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, n)
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if (x.Sel.Name == "WithContext" || x.Sel.Name == "Session") && (!isFunc || fn.Name.Name != "stoppingDB") {
						bad = append(bad, fset.Position(x.Pos()).String()+" "+x.Sel.Name)
					}
				case *ast.AssignStmt:
					for _, lhs := range x.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && (sel.Sel.Name == "db" || sel.Sel.Name == "kv") {
							bad = append(bad, fset.Position(x.Pos()).String()+" assigns "+sel.Sel.Name)
						}
					}
				case *ast.Ident:
					if !isFunc || fn.Name.Name != "NewHub" || (x.Name != "db" && x.Name != "kvStore") || len(stack) < 2 {
						return true
					}
					if _, isField := stack[len(stack)-2].(*ast.Field); isField {
						return true
					}
					if kv, isKey := stack[len(stack)-2].(*ast.KeyValueExpr); isKey && kv.Key == x {
						return true
					}
					if sel, isSel := stack[len(stack)-2].(*ast.SelectorExpr); isSel && sel.Sel == x {
						return true
					}
					call, isCall := stack[len(stack)-2].(*ast.CallExpr)
					if !isCall || !allowed[callName(call)] {
						bad = append(bad, fset.Position(x.Pos()).String()+" uses "+x.Name)
					}
				}
				return true
			})
		}
	}
	require.Empty(t, bad, "ways to the stores of the hub other than its stop context")
}

func callName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
	}
	return ""
}
