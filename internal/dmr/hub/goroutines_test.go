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
