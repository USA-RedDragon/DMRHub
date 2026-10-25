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

package plugins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var guests struct { //nolint:gochecknoglobals // built once for all the tests
	once sync.Once
	dir  string
	err  error
}

// guest returns the test plugin testdata/name of the wasm package, built
// for wasip1.
func guest(t *testing.T, name string) []byte {
	t.Helper()
	guests.once.Do(func() {
		guests.dir, guests.err = os.MkdirTemp("", "dmrhub-api-guests-")
		if guests.err != nil {
			return
		}
		for _, g := range []string{"echo", "netprobe"} {
			cmd := exec.Command("go", "build", "-o", filepath.Join(guests.dir, g+".wasm"), "../../../../../plugins/wasm/testdata/"+g) //nolint:noctx // a build for the tests
			cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
			if out, err := cmd.CombinedOutput(); err != nil {
				guests.err = fmt.Errorf("could not build %s: %w\n%s", g, err, out)
				return
			}
		}
	})
	require.NoError(t, guests.err)
	b, err := os.ReadFile(filepath.Join(guests.dir, name+".wasm"))
	require.NoError(t, err)
	return b
}

func TestMain(m *testing.M) {
	code := m.Run()
	if guests.dir != "" {
		_ = os.RemoveAll(guests.dir)
	}
	os.Exit(code)
}

// upload sends file and fields as a multipart form.
func (e *env) upload(jar testutils.CookieJar, path string, file []byte, fields map[string]string, out any) (int, string) {
	e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if file != nil {
		f, err := w.CreateFormFile("file", "plugin.wasm")
		require.NoError(e.t, err)
		_, err = f.Write(file)
		require.NoError(e.t, err)
	}
	for k, v := range fields {
		require.NoError(e.t, w.WriteField(k, v))
	}
	require.NoError(e.t, w.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, &body)
	require.NoError(e.t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for _, c := range jar.Cookies() {
		req.Header.Add("Cookie", c.String())
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code, rec.Body.String()
}

func TestTheListShowsWhereEachPluginRuns(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, _, err := models.CreatePlugin(e.db, "Weather", []models.PluginRange{{First: 9200, Last: 9200}}, time.Now())
	require.NoError(t, err)
	_, err = models.EnsureBuiltinPlugin(e.db, "parrot", "Parrot", "v1.3.4", []models.PluginRange{{First: 9990, Last: 9990}}, time.Now())
	require.NoError(t, err)
	_, err = models.CreateUploadedPlugin(e.db, models.PluginInfo{
		Name: "Net check-in", Version: "0.4.1", Author: "N1CALL", Description: "Logs each radio.",
		Hosts: []models.PluginHost{{Address: "nets.example.org:443", Reason: "Posts the list."}},
	}, []models.PluginRange{{First: 9100, Last: 9100}}, []string{"nets.example.org:443"}, []byte("wasm"), time.Now())
	require.NoError(t, err)

	var list apimodels.PluginList
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodGet, "/api/v1/admin/plugins", nil, &list))
	assert.True(t, list.InProcess)
	byName := map[string]apimodels.AdminPlugin{}
	for _, p := range list.Plugins {
		byName[p.Name] = p
	}
	assert.Equal(t, "external", byName["Weather"].Kind)
	assert.Equal(t, "builtin", byName["Parrot"].Kind)
	assert.Equal(t, "v1.3.4", byName["Parrot"].Version)
	up := byName["Net check-in"]
	assert.Equal(t, "uploaded", up.Kind)
	assert.Equal(t, "0.4.1", up.Version)
	assert.Equal(t, "N1CALL", up.Author)
	assert.Equal(t, "Logs each radio.", up.Description)
	assert.Equal(t, []apimodels.PluginHost{{Address: "nets.example.org:443", Reason: "Posts the list.", Allowed: true}}, up.Hosts)
	assert.NotNil(t, byName["Weather"].Hosts, "a plugin without hosts shows an empty list")
}

func TestInspectingAFileShowsWhatThePluginAsks(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var m apimodels.PluginManifest
	code, body := e.upload(e.admin, "/api/v1/admin/plugins/inspect", guest(t, "netprobe"), nil, &m)
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "Netprobe", m.Name)
	assert.Equal(t, []apimodels.PluginRange{{First: 9200, Last: 9200}}, m.Ranges)
	assert.Equal(t, []apimodels.PluginHost{{Address: "allowed.example.org:7", Reason: "Echoes."}}, m.Hosts)
	list, err := models.ListPlugins(e.db)
	require.NoError(t, err)
	assert.Empty(t, list, "inspecting adds nothing")

	code, body = e.upload(e.admin, "/api/v1/admin/plugins/inspect", []byte("#!/bin/sh\n"), nil, nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body, "Choose a .wasm plugin file.")
	code, _ = e.upload(e.admin, "/api/v1/admin/plugins/inspect", nil, nil, nil)
	assert.Equal(t, http.StatusBadRequest, code)
	testutils.SeedAccount(t, e.db, 1000001, "KI5VMF", "user")
	user := testutils.Login(t, e.router, "user")
	code, _ = e.upload(user, "/api/v1/admin/plugins/inspect", guest(t, "netprobe"), nil, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestUploadingAPluginAddsItWithTheIDsAndHostsTheAdminChose(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var p apimodels.AdminPlugin
	code, body := e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"),
		map[string]string{"ranges": `[{"first":9200,"last":9201}]`, "allowed_hosts": `["allowed.example.org:7"]`}, &p)
	require.Equal(t, http.StatusCreated, code, body)
	assert.Equal(t, "Netprobe", p.Name)
	assert.Equal(t, "uploaded", p.Kind)
	assert.True(t, p.Enabled)
	assert.Equal(t, []apimodels.PluginRange{{First: 9200, Last: 9201}}, p.Ranges)
	assert.Equal(t, []apimodels.PluginHost{{Address: "allowed.example.org:7", Reason: "Echoes.", Allowed: true}}, p.Hosts)
	_, wasm, err := models.PluginModuleOf(e.db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, guest(t, "netprobe"), wasm)

	code, _ = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"), map[string]string{"ranges": `[{"first":9300,"last":9300}]`}, nil)
	assert.Equal(t, http.StatusBadRequest, code, "another plugin has the name")
	code, _ = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "echo"), map[string]string{"ranges": `[{"first":9200,"last":9200}]`}, nil)
	assert.Equal(t, http.StatusBadRequest, code, "another plugin has the IDs")
	code, _ = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "echo"), map[string]string{"ranges": "nope"}, nil)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestANewVersionReplacesTheModuleOfAnUploadedPlugin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var p apimodels.AdminPlugin
	code, body := e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"),
		map[string]string{"ranges": `[{"first":9200,"last":9200}]`, "allowed_hosts": `["allowed.example.org:7"]`}, &p)
	require.Equal(t, http.StatusCreated, code, body)
	var next apimodels.AdminPlugin
	code, body = e.upload(e.admin, fmt.Sprintf("/api/v1/admin/plugins/%d/module", p.ID), guest(t, "echo"), nil, &next)
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "1.0.0", next.Version)
	assert.Equal(t, "Netprobe", next.Name, "the name stays")
	assert.Equal(t, []apimodels.PluginRange{{First: 9200, Last: 9200}}, next.Ranges, "the IDs stay")
	assert.Empty(t, next.Hosts)

	builtin, err := models.EnsureBuiltinPlugin(e.db, "parrot", "Parrot", "v1.3.4", nil, time.Now())
	require.NoError(t, err)
	code, _ = e.upload(e.admin, fmt.Sprintf("/api/v1/admin/plugins/%d/module", builtin.ID), guest(t, "echo"), nil, nil)
	assert.Equal(t, http.StatusBadRequest, code, "a built-in plugin has no new version to upload")
	code, _ = e.upload(e.admin, "/api/v1/admin/plugins/999/module", guest(t, "echo"), nil, nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestTheAdminAllowsTheHostsOfAPlugin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p, err := models.CreateUploadedPlugin(e.db, models.PluginInfo{Name: "Net check-in",
		Hosts: []models.PluginHost{{Address: "nets.example.org:443"}, {Address: "time.example.org:123"}}}, nil, nil, []byte("wasm"), time.Now())
	require.NoError(t, err)
	var got apimodels.AdminPlugin
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodPut, fmt.Sprintf("/api/v1/admin/plugins/%d", p.ID),
		map[string]any{"name": "Net check-in", "enabled": true, "ranges": []map[string]uint{{"first": 9100, "last": 9100}},
			"allowed_hosts": []string{"time.example.org:123"}}, &got))
	assert.Equal(t, []apimodels.PluginHost{{Address: "nets.example.org:443"}, {Address: "time.example.org:123", Allowed: true}}, got.Hosts)
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodPut, fmt.Sprintf("/api/v1/admin/plugins/%d", p.ID),
		map[string]any{"name": "Net check-in", "enabled": true, "ranges": []map[string]uint{{"first": 9100, "last": 9100}}}, &got))
	assert.True(t, got.Hosts[1].Allowed, "a change without allowed_hosts leaves the hosts alone")
}

func TestABuiltinPluginIsNeitherRemovedNorGivenAToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	builtin, err := models.EnsureBuiltinPlugin(e.db, "parrot", "Parrot", "v1.3.4", nil, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, e.do(e.admin, http.MethodDelete, fmt.Sprintf("/api/v1/admin/plugins/%d", builtin.ID), nil, nil))
	assert.Equal(t, http.StatusBadRequest, e.do(e.admin, http.MethodPost, fmt.Sprintf("/api/v1/admin/plugins/%d/token", builtin.ID), nil, nil))
	_, err = models.FindPlugin(e.db, builtin.ID)
	require.NoError(t, err)
}

func TestAPluginFileOver32MiBIsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	big := make([]byte, 32<<20+1)
	copy(big, "\x00asm")
	code, _ := e.upload(e.admin, "/api/v1/admin/plugins/inspect", big, nil, nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
}
