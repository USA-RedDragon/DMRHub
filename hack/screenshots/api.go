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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

type api struct {
	base    string
	client  *http.Client
	header  map[string]string
	secrets *secrets
	before  func()
}

func newAPI(base string, sec *secrets) *api {
	jar, _ := cookiejar.New(nil)
	return &api{base: base, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}, secrets: sec}
}

func (a *api) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
		r = bytes.NewReader(b)
	}
	if a.before != nil {
		a.before()
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, r)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.header {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(data))
	}
	var all any
	if json.Unmarshal(data, &all) == nil {
		a.secrets.scan(all)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: bad answer: %w", method, path, err)
		}
	}
	return nil
}

func (a *api) waitUp(ctx context.Context) error {
	return a.waitFor(ctx, "/api/v1/ping")
}

func (a *api) waitFor(ctx context.Context, path string) error {
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		if err := a.do(ctx, http.MethodGet, path, nil, nil); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s: %w", a.base, errTimeout)
}

func (a *api) login(ctx context.Context, username, password string) error {
	return a.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password}, nil)
}
