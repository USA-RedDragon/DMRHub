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

// Package dbtest has helpers for testing the RadioID.net backed databases without the network.
package dbtest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/ulikunitz/xz"
)

// Compress returns data compressed with xz, the format of the embedded databases.
func Compress(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("xz.NewWriter: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("xz write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("xz close: %v", err)
	}
	return buf.Bytes()
}

// Decompress returns the contents of an xz-compressed database.
func Decompress(t testing.TB, data []byte) []byte {
	t.Helper()
	r, err := xz.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("xz.NewReader: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("xz read: %v", err)
	}
	return out
}

// Server is an HTTP server standing in for RadioID.net.
type Server struct {
	URL  string
	hits atomic.Int32
}

// Hits returns the number of requests the server has received.
func (s *Server) Hits() int {
	return int(s.hits.Load())
}

// NewServer serves body, answering the first failures requests with 503 Service Unavailable.
func NewServer(t testing.TB, failures int, body []byte) *Server {
	t.Helper()
	s := &Server{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if int(s.hits.Add(1)) <= failures {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// NewTruncatedServer advertises the full length of body but drops the connection halfway through it.
func NewTruncatedServer(t testing.TB, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:len(body)/2])
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
