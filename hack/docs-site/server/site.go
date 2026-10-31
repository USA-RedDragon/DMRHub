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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "public, max-age=0, must-revalidate"
	cacheDay        = "public, max-age=86400"
	notFoundPage    = "404.html"
	shellPrefix     = "docs/_shell/"
)

type file struct {
	data    []byte
	zstd    []byte
	etag    string
	modTime time.Time
}

type site struct {
	files map[string]*file
	dirs  map[string]bool
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func cacheControl(name string) string {
	switch {
	case strings.HasPrefix(name, shellPrefix):
		return cacheImmutable
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".xml"):
		return cacheRevalidate
	}
	return cacheDay
}

func loadSite(fsys fs.FS) (*site, error) {
	s := &site{files: map[string]*file{}, dirs: map[string]bool{}}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			s.dirs[name] = true
			return nil
		}
		if strings.HasSuffix(name, ".zst") {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		f := &file{data: data, etag: `"` + hex.EncodeToString(sum[:8]) + `"`, modTime: info.ModTime()}
		if z, err := fs.ReadFile(fsys, name+".zst"); err == nil && len(z) < len(data) {
			f.zstd = z
		}
		s.files[name] = f
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk the site: %w", err)
	}
	if s.files["index.html"] == nil {
		return nil, fmt.Errorf("the site has no index.html: %w", fs.ErrNotExist)
	}
	return s, nil
}

func acceptsZstd(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		token, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(token), "zstd") {
			continue
		}
		name, value, ok := strings.Cut(strings.TrimSpace(params), "=")
		if !ok || strings.TrimSpace(name) != "q" {
			return true
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return err == nil && q > 0
	}
	return false
}

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.URL.RawQuery != "" {
		to += "?" + r.URL.RawQuery
	}
	w.Header().Set("Cache-Control", cacheDay)
	http.Redirect(w, r, to, http.StatusMovedPermanently)
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	switch {
	case p == "/":
		s.serve(w, r, "index.html", http.StatusOK)
		return
	case p == "/docs" || p == "/docs/":
		redirect(w, r, "/")
		return
	case !strings.HasPrefix(p, "/docs/"), path.Clean(p) != strings.TrimSuffix(p, "/"):
		s.serve(w, r, notFoundPage, http.StatusNotFound)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if strings.HasSuffix(name, "/") {
		name += "index.html"
	} else if s.dirs[name] && s.files[name+"/index.html"] != nil {
		redirect(w, r, p+"/")
		return
	}
	if s.files[name] == nil {
		s.serve(w, r, notFoundPage, http.StatusNotFound)
		return
	}
	s.serve(w, r, name, http.StatusOK)
}

func (s *site) serve(w http.ResponseWriter, r *http.Request, name string, status int) {
	f := s.files[name]
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	if f == nil {
		http.Error(w, http.StatusText(status), status)
		return
	}
	h.Set("Content-Type", contentType(name))
	body, etag := f.data, f.etag
	if f.zstd != nil {
		h.Set("Vary", "Accept-Encoding")
		if acceptsZstd(r.Header.Get("Accept-Encoding")) {
			h.Set("Content-Encoding", "zstd")
			body, etag = f.zstd, strings.TrimSuffix(f.etag, `"`)+`-zstd"`
		}
	}
	if status != http.StatusOK {
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}
	h.Set("Cache-Control", cacheControl(name))
	h.Set("ETag", etag)
	http.ServeContent(w, r, name, f.modTime, bytes.NewReader(body))
}
