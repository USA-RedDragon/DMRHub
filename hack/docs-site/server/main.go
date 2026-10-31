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

// Command docs-server serves the dmrhub.net docs site that
// hack/docs-site/build.py writes: the DMRHub docs home at /, the other
// pages under /docs/, and /docs/sitemap.xml. /docs/ redirects to /, and a
// page address without its trailing slash redirects to the address with
// it. When the client accepts zstd and the site has a .zst copy of a
// file, the server sends that copy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	root := flag.String("root", "/site", "directory of the built site")
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()
	if err := run(*root, *addr); err != nil {
		slog.Error("docs-server stopped", "error", err)
		os.Exit(1)
	}
}

func run(root, addr string) error {
	site, err := loadSite(os.DirFS(root))
	if err != nil {
		return fmt.Errorf("read the site in %s: %w", root, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              addr,
		Handler:           site,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	errs := make(chan error, 1)
	go func() {
		slog.Info("serving the docs site", "addr", addr, "root", root, "files", len(site.files))
		errs <- srv.ListenAndServe()
	}()

	select {
	case err := <-errs:
		return fmt.Errorf("listen on %s: %w", addr, err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shut down: %w", err)
	}
	return nil
}
