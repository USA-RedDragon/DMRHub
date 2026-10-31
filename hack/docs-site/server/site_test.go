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
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testSite(t *testing.T) *site {
	t.Helper()
	s, err := loadSite(fstest.MapFS{
		"index.html":                           {Data: []byte("home")},
		"404.html":                             {Data: []byte("missing")},
		"docs/sitemap.xml":                     {Data: []byte("<urlset/>")},
		"docs/faq/index.html":                  {Data: []byte("faq page, long enough to compress")},
		"docs/faq/index.html.zst":              {Data: []byte("z")},
		"docs/dmrgo/start/install/index.html":  {Data: []byte("install")},
		"docs/_shell/shell.0123456789ab.css":   {Data: []byte("body{}")},
		"docs/_shell/font.0123456789ab.woff2":  {Data: []byte("font")},
		"docs/Screenshots/lastheard.png":       {Data: []byte("png")},
		"docs/dmrgo/machines/states/trunk.svg": {Data: []byte("<svg/>")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServe(t *testing.T) {
	t.Parallel()
	s := testSite(t)
	cases := []struct {
		method, path, encoding string
		status                 int
		location, ctype, cache string
		body, contentEncoding  string
	}{
		{path: "/", status: http.StatusOK, ctype: "text/html; charset=utf-8", cache: cacheRevalidate, body: "home"},
		{path: "/docs/", status: http.StatusMovedPermanently, location: "/"},
		{path: "/docs", status: http.StatusMovedPermanently, location: "/"},
		{path: "/docs/?q=x", status: http.StatusMovedPermanently, location: "/?q=x"},
		{path: "/docs/faq", status: http.StatusMovedPermanently, location: "/docs/faq/"},
		{path: "/docs/faq/", status: http.StatusOK, ctype: "text/html; charset=utf-8", body: "faq page, long enough to compress"},
		{path: "/docs/faq/", encoding: "gzip, deflate, br, zstd", status: http.StatusOK, body: "z", contentEncoding: "zstd"},
		{path: "/docs/faq/", encoding: "zstd;q=0", status: http.StatusOK, body: "faq page, long enough to compress"},
		{path: "/docs/faq/index.html", status: http.StatusOK, body: "faq page, long enough to compress"},
		{path: "/docs/dmrgo/start/install", status: http.StatusMovedPermanently, location: "/docs/dmrgo/start/install/"},
		{path: "/docs/dmrgo/start/install/", status: http.StatusOK, body: "install"},
		{path: "/docs/sitemap.xml", status: http.StatusOK, ctype: "application/xml; charset=utf-8", cache: cacheRevalidate},
		{path: "/docs/_shell/shell.0123456789ab.css", status: http.StatusOK, ctype: "text/css; charset=utf-8", cache: cacheImmutable},
		{path: "/docs/_shell/font.0123456789ab.woff2", status: http.StatusOK, ctype: "font/woff2", cache: cacheImmutable},
		{path: "/docs/Screenshots/lastheard.png", status: http.StatusOK, ctype: "image/png", cache: cacheDay},
		{path: "/docs/dmrgo/machines/states/trunk.svg", status: http.StatusOK, ctype: "image/svg+xml"},
		{path: "/docs/_shell/", status: http.StatusNotFound, body: "missing", cache: "no-store"},
		{path: "/docs/nope/", status: http.StatusNotFound, body: "missing"},
		{path: "/docs/dmrgo/machines/states", status: http.StatusNotFound},
		{path: "/docs//faq/", status: http.StatusNotFound},
		{path: "/docs/faq/../faq/", status: http.StatusNotFound},
		{path: "/index.html", status: http.StatusNotFound},
		{path: "/lastheard", status: http.StatusNotFound},
		{method: http.MethodPost, path: "/", status: http.StatusMethodNotAllowed},
		{method: http.MethodHead, path: "/docs/faq/", status: http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.method+c.path+c.encoding, func(t *testing.T) {
			t.Parallel()
			method := c.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequestWithContext(t.Context(), method, c.path, nil)
			if c.encoding != "" {
				req.Header.Set("Accept-Encoding", c.encoding)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			res := w.Result()
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != c.status {
				t.Fatalf("status %d, want %d", res.StatusCode, c.status)
			}
			if got := res.Header.Get("Location"); got != c.location {
				t.Errorf("Location %q, want %q", got, c.location)
			}
			if got := res.Header.Get("Content-Type"); c.ctype != "" && got != c.ctype {
				t.Errorf("Content-Type %q, want %q", got, c.ctype)
			}
			if got := res.Header.Get("Cache-Control"); c.cache != "" && got != c.cache {
				t.Errorf("Cache-Control %q, want %q", got, c.cache)
			}
			if got := res.Header.Get("Content-Encoding"); got != c.contentEncoding {
				t.Errorf("Content-Encoding %q, want %q", got, c.contentEncoding)
			}
			if got := w.Body.String(); c.body != "" && got != c.body {
				t.Errorf("body %q, want %q", got, c.body)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Errorf("HEAD sent a body of %d bytes", w.Body.Len())
			}
		})
	}
}

func TestNotModified(t *testing.T) {
	t.Parallel()
	s := testSite(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/docs/faq/", nil))
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/docs/faq/", nil)
	req.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified {
		t.Fatalf("status %d, want 304", w.Code)
	}
}

func TestLoadSiteNeedsIndex(t *testing.T) {
	t.Parallel()
	if _, err := loadSite(fstest.MapFS{"docs/faq/index.html": {Data: []byte("x")}}); err == nil {
		t.Fatal("loadSite accepted a site without index.html")
	}
}

func TestAcceptsZstd(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]bool{
		"":                     false,
		"gzip":                 false,
		"zstd":                 true,
		"gzip, zstd":           true,
		"ZSTD":                 true,
		"zstd;q=0":             false,
		"zstd; q=0.5":          true,
		"zstd;q=0.000":         false,
		"gzip;q=1, zstd;q=bad": false,
		"br, gzip, deflate, *": false,
		"zstdx, gzip":          false,
		"gzip, zstd;level=3":   true,
		" zstd ; q=1 , gzip":   true,
	} {
		if got := acceptsZstd(header); got != want {
			t.Errorf("acceptsZstd(%q) = %v, want %v", header, got, want)
		}
	}
}
