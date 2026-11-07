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

package http_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

type route struct {
	method string
	segs   []string
}

var (
	apiCallRe    = regexp.MustCompile(`API\.(get|post|patch|put|delete)(?:<[^>]*>)?\(`)
	interceptRe  = regexp.MustCompile(`cy\.intercept\(\s*'(GET|POST|PATCH|PUT|DELETE)'\s*,\s*'(/api/v1/[^']*)'`)
	interpolRe   = regexp.MustCompile(`\$\{[^}]*\}`)
	quotedPartRe = regexp.MustCompile("^\\s*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`)\\s*$")
)

func splitPath(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}

func (r route) matches(method string, segs []string) bool {
	if r.method != method || len(r.segs) != len(segs) {
		return false
	}
	for i, s := range r.segs {
		if strings.HasPrefix(s, ":") || segs[i] == "*" {
			continue
		}
		if s != segs[i] {
			return false
		}
	}
	return true
}

func firstArg(src string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return src[:i]
			}
			depth--
		case c == ',' && depth == 0:
			return src[:i]
		}
	}
	return src
}

// callPath turns the first argument of an API call into a path, with "*" for
// every segment built at run time. It returns false for an argument that has
// no string literal to read.
func callPath(arg string) (string, bool) {
	var b strings.Builder
	literal := false
	for _, part := range strings.Split(arg, "+") {
		m := quotedPartRe.FindStringSubmatch(part)
		if m == nil {
			b.WriteString("*")
			continue
		}
		literal = true
		s := m[1] + m[2] + m[3]
		s = interpolRe.ReplaceAllStringFunc(s, func(string) string { return "\x00" })
		b.WriteString(s)
	}
	if !literal {
		return "", false
	}
	p := b.String()
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	segs := splitPath(p)
	for i, s := range segs {
		if s == "\x00" {
			segs[i] = "*"
		} else {
			segs[i] = strings.TrimRight(s, "\x00")
		}
	}
	return "/" + strings.Join(segs, "/"), true
}

// TestFrontendCallsMatchRoutes checks every API call in the frontend source
// and every e2e intercept against the routes the server serves, so a changed
// method or path on one side cannot hide behind a stubbed test.
func TestFrontendCallsMatchRoutes(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	var routes []route
	for _, r := range router.Routes() {
		if p, ok := strings.CutPrefix(r.Path, "/api/v1"); ok {
			routes = append(routes, route{method: r.Method, segs: splitPath(p)})
		}
	}
	require.NotEmpty(t, routes)

	served := func(method, path string) bool {
		segs := splitPath(path)
		for _, r := range routes {
			if r.matches(method, segs) {
				return true
			}
		}
		return false
	}

	checked := 0
	walk := func(dir string, check func(file, src string)) {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if ext := filepath.Ext(path); ext != ".ts" && ext != ".vue" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			check(path, string(src))
			return nil
		})
		require.NoError(t, err)
	}

	walk(filepath.Join("frontend", "src"), func(file, src string) {
		for _, loc := range apiCallRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[loc[2]:loc[3]])
			path, ok := callPath(firstArg(src[loc[1]:]))
			if !ok || strings.HasPrefix(path, "/setupwizard") {
				continue
			}
			checked++
			if !served(method, path) {
				t.Errorf("%s calls %s %s, which the server does not serve", file, method, path)
			}
		}
	})

	walk(filepath.Join("frontend", "tests"), func(file, src string) {
		for _, m := range interceptRe.FindAllStringSubmatch(src, -1) {
			p := strings.TrimPrefix(m[2], "/api/v1")
			if i := strings.IndexByte(p, '?'); i >= 0 {
				p = p[:i]
			}
			p = strings.TrimRight(p, "*")
			checked++
			if !served(m[1], p) {
				t.Errorf("%s stubs %s /api/v1%s, which the server does not serve", file, m[1], p)
			}
		}
	})

	require.Greater(t, checked, 150)
}
