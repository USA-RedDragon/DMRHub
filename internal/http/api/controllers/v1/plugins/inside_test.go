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
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
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
		for _, g := range []string{"echo", "netprobe", "early"} {
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

func TestTheListSaysWhatEachPluginInsideDoes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now()
	parrot, err := models.EnsureBuiltinPlugin(e.db, "parrot", "Parrot", "v1.3.12", []models.PluginRange{{First: 9990, Last: 9990}}, now)
	require.NoError(t, err)
	require.NoError(t, models.NotePluginConnected(e.db, parrot.ID, "s1", "", now))
	require.NoError(t, models.NotePluginSeen(e.db, parrot.ID, "s1", now))
	starting, err := models.CreateUploadedPlugin(e.db, models.PluginInfo{Name: "Net check-in", Version: "0.4.1"},
		[]models.PluginRange{{First: 9100, Last: 9100}}, nil, []byte("wasm"), now)
	require.NoError(t, err)
	weather, err := models.CreateUploadedPlugin(e.db, models.PluginInfo{Name: "Weather", Version: "1.2.0"},
		[]models.PluginRange{{First: 9200, Last: 9200}}, nil, []byte("wasm"), now)
	require.NoError(t, err)
	reason := "It used more memory than a plugin may (64 MiB)."
	stoppedAt := now.Add(-10 * time.Second).Truncate(time.Second)
	require.NoError(t, plugins.NoteStop(context.Background(), e.kv, weather.ID, now.Add(-20*time.Minute), now.Add(-19*time.Minute), "It crashed: boom."))
	require.NoError(t, plugins.NoteStop(context.Background(), e.kv, weather.ID, stoppedAt, stoppedAt.Add(40*time.Second), reason))
	old, _, err := models.CreatePlugin(e.db, "Old locator", []models.PluginRange{{First: 5052, Last: 5052}}, now)
	require.NoError(t, err)
	locator, err := models.EnsureBuiltinPlugin(e.db, "locator", "Location server", "v1.3.12", []models.PluginRange{{First: 5050, Last: 5059}}, now)
	require.NoError(t, err)
	off, err := models.CreateUploadedPlugin(e.db, models.PluginInfo{Name: "Echo test", Version: "0.1.0"},
		[]models.PluginRange{{First: 9300, Last: 9300}}, nil, []byte("wasm"), now)
	require.NoError(t, err)
	_, err = models.UpdatePlugin(e.db, off.ID, off.Name, false, off.Ranges)
	require.NoError(t, err)

	var list apimodels.PluginList
	require.Equal(t, http.StatusOK, e.do(e.admin, http.MethodGet, "/api/v1/admin/plugins", nil, &list))
	byID := map[uint]apimodels.AdminPlugin{}
	for _, p := range list.Plugins {
		byID[p.ID] = p
	}
	assert.Equal(t, apimodels.PluginRunning, byID[parrot.ID].State)
	assert.Equal(t, apimodels.PluginStarting, byID[starting.ID].State)
	assert.Equal(t, apimodels.PluginOff, byID[off.ID].State)
	assert.Empty(t, byID[old.ID].State, "a plugin that runs as its own program has no such state")

	w := byID[weather.ID]
	assert.Equal(t, apimodels.PluginStopped, w.State)
	require.NotNil(t, w.StoppedAt)
	require.NotNil(t, w.RestartAt)
	assert.WithinDuration(t, stoppedAt, *w.StoppedAt, time.Second)
	assert.WithinDuration(t, stoppedAt.Add(40*time.Second), *w.RestartAt, time.Second)
	assert.Equal(t, reason, w.StopReason)
	assert.Equal(t, 2, w.StopsLastHour)

	l := byID[locator.ID]
	assert.Equal(t, apimodels.PluginWaiting, l.State)
	assert.Equal(t, uint(5052), l.WaitingID)
	assert.Equal(t, "Old locator", l.WaitingFor)
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
	assert.Contains(t, body, "This file is not a WebAssembly module.")
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
	code, body := e.upload(e.admin, "/api/v1/admin/plugins/inspect", big, nil, nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, []problem{{Field: "file", Message: "This file is 33 MiB. A plugin can be at most 32 MiB."}}, refusal(t, body).Errors)
}

type problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type refused struct {
	Error    string                    `json:"error"`
	Errors   []problem                 `json:"errors"`
	Manifest *apimodels.PluginManifest `json:"manifest"`
}

func refusal(t *testing.T, body string) refused {
	t.Helper()
	var r refused
	require.NoError(t, json.Unmarshal([]byte(body), &r), body)
	require.NotEmpty(t, r.Errors, body)
	assert.Equal(t, r.Errors[0].Message, r.Error, "error is the first problem")
	return r
}

func TestAFileThatIsNoPluginIsRefusedWithWhy(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	notWasm := "This file is not a WebAssembly module. Choose the .wasm file that the plugin's build made."
	for _, c := range []struct {
		want string
		file []byte
	}{
		{notWasm, []byte("#!/bin/sh\n")},
		{notWasm, []byte("\x00asm\x01\x00\x00\x00\x0a\x05")},
		{"This file is WebAssembly but not a DMRHub plugin. Plugins are built with GOOS=wasip1 GOARCH=wasm.", []byte("\x00asm\x01\x00\x00\x00")},
		{"The plugin stopped before it introduced itself: panic: no config.", guest(t, "early")},
	} {
		want := c.want
		code, body := e.upload(e.admin, "/api/v1/admin/plugins/inspect", c.file, nil, nil)
		assert.Equal(t, http.StatusBadRequest, code, want)
		r := refusal(t, body)
		assert.Equal(t, []problem{{Field: "file", Message: want}}, r.Errors)
		assert.Nil(t, r.Manifest)
	}
}

func TestEachProblemOfAPluginIsUnderItsField(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := models.EnsureBuiltinPlugin(e.db, "parrot", "Parrot", "v1.3.12", []models.PluginRange{{First: 9200, Last: 9200}}, time.Now())
	require.NoError(t, err)
	code, body := e.upload(e.admin, "/api/v1/admin/plugins/inspect", guest(t, "netprobe"), nil, nil)
	require.Equal(t, http.StatusBadRequest, code, body)
	r := refusal(t, body)
	assert.Equal(t, []problem{{Field: "ids", Message: "9200 is already used by Parrot. Change the IDs, or turn Parrot off first."}}, r.Errors)
	require.NotNil(t, r.Manifest, "the admin sees what the plugin asks, to change its IDs")
	assert.Equal(t, "Netprobe", r.Manifest.Name)

	code, body = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"), map[string]string{"ranges": `[{"first":0,"last":0}]`}, nil)
	require.Equal(t, http.StatusBadRequest, code, body)
	assert.Equal(t, []problem{{Field: "ids", Message: "The plugin asks for ID 0, which is not a radio or talkgroup ID."}}, refusal(t, body).Errors)

	var p apimodels.AdminPlugin
	code, body = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"), map[string]string{"ranges": `[{"first":9201,"last":9201}]`}, &p)
	require.Equal(t, http.StatusCreated, code, body)
	code, body = e.upload(e.admin, "/api/v1/admin/plugins/upload", guest(t, "netprobe"), map[string]string{"ranges": `[{"first":9202,"last":9202}]`}, nil)
	require.Equal(t, http.StatusBadRequest, code, body)
	assert.Equal(t, []problem{{Field: "file", Message: "Netprobe is already uploaded. Open it and choose “Upload a new version”."}}, refusal(t, body).Errors)
}
