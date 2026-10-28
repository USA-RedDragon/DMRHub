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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const radioURL = "http://127.0.0.1:4180"

func startRadio(ctx context.Context, dir string) (*http.Server, error) {
	if dir == "" {
		return nil, errors.New("-radio is required: a built dmr-web (its dist directory)")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return nil, fmt.Errorf("no dmr-web build in %s: %w", dir, err)
	}
	files := http.FileServer(http.Dir(dir))
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
			if st, err := os.Stat(p); err != nil || st.IsDir() && r.URL.Path != "/" {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
			files.ServeHTTP(w, r)
		}),
	}
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", strings.TrimPrefix(radioURL, "http://"))
	if err != nil {
		return nil, fmt.Errorf("could not serve dmr-web: %w", err)
	}
	go func() { _ = srv.Serve(l) }()
	return srv, nil
}
