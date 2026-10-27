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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wizardRequest(t *testing.T, r *gin.Engine, method string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, "/api/v1/config", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestSetupWizardSavesToTheDatabase runs the setup wizard of a hub whose
// deployment sets its secrets but not its URL. The wizard saves the URL in
// the database, where the next start finds it, and refuses a change of a
// secret, which only the deployment sets.
func TestSetupWizardSavesToTheDatabase(t *testing.T) {
	t.Parallel()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&base)
	base.Secret = "a-secret-that-is-long-enough-for-the-check"
	base.PasswordSalt = "a-salt-that-is-long-enough-for-the-check"
	database, err := db.MakeDB(&base)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	live := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
	require.NoError(t, live.Boot(context.Background()))
	require.Error(t, live.Current().Get().Validate())

	state := setupwizard.NewState()
	state.SetLive(live, database)
	r := internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test")
	w := wizardRequest(t, r, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	httpSection, ok := cfg["http"].(map[string]any)
	require.True(t, ok)
	httpSection["canonical-host"] = "https://hub.example"

	cfg["secret"] = "another-secret-that-is-long-enough"
	w = wizardRequest(t, r, http.MethodPut, cfg)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "secret")

	delete(cfg, "secret")
	w = wizardRequest(t, r, http.MethodPut, cfg)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, live.Current().Get().Validate())

	next := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
	require.NoError(t, next.Boot(context.Background()))
	assert.Equal(t, "https://hub.example", next.Current().Get().HTTP.CanonicalHost)
}

type wizardConfig struct {
	Settings map[string]liveconfig.Setting `json:"settings"`
	Wizard   *struct {
		Phase          string `json:"phase"`
		DatabaseError  string `json:"database_error"`
		DatabaseLocked bool   `json:"database_locked"`
		DatabaseFile   string `json:"database_file"`
		StoreError     string `json:"store_error"`
	} `json:"wizard"`
}

// brokenDatabase returns a config whose SQLite database is in a directory
// that does not exist, the error of its connection, and a wizard router in
// the database phase.
func brokenDatabase(t *testing.T, locks liveconfig.Locks, store liveconfig.DatabaseState) (*liveconfig.DatabaseFix, *gin.Engine) {
	t.Helper()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "missing", "DMRHub.db")
	_, openErr := db.MakeDB(&base)
	require.Error(t, openErr)
	fix := liveconfig.NewDatabaseFix(&base, locks, store, db.MakeDB, openErr)
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	return fix, internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test")
}

func wizardGET(t *testing.T, r *gin.Engine) (map[string]any, wizardConfig) {
	t.Helper()
	w := wizardRequest(t, r, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	var cfg wizardConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	return raw, cfg
}

func setDatabase(raw map[string]any, path string) {
	d, _ := raw["database"].(map[string]any)
	d["database"] = path
}

// TestSetupWizardFixesTheDatabase starts the wizard of a hub that cannot
// reach its database. Only the database settings can change. A setting
// that still fails stays refused; one that works reaches the start of
// DMRHub and stays in the state file, readable by its owner only.
func TestSetupWizardFixesTheDatabase(t *testing.T) {
	t.Parallel()
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	fix, r := brokenDatabase(t, liveconfig.Locks{}, store)

	assert.Empty(t, wizardClosed(t, r), "a new hub gets the database step")
	raw, cfg := wizardGET(t, r)
	require.NotNil(t, cfg.Wizard)
	assert.Equal(t, liveconfig.PhaseDatabase, cfg.Wizard.Phase)
	assert.NotEmpty(t, cfg.Wizard.DatabaseError)
	assert.Equal(t, store.Path, cfg.Wizard.DatabaseFile)
	assert.Empty(t, cfg.Wizard.StoreError)
	assert.True(t, cfg.Settings["database.database"].Editable)
	assert.False(t, cfg.Settings["network-name"].Editable)

	name := raw["network-name"]
	raw["network-name"] = "Too early"
	w := wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "fix the database first")
	raw["network-name"] = name

	setDatabase(raw, filepath.Join(t.TempDir(), "still", "missing.db"))
	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "cannot reach this database")
	_, err := os.Stat(store.Path)
	assert.ErrorIs(t, err, os.ErrNotExist, "a setting that fails is not kept")

	good := filepath.Join(t.TempDir(), "DMRHub.db")
	setDatabase(raw, good)
	got := make(chan liveconfig.Fixed, 1)
	go func() {
		f := <-fix.Fixed()
		got <- f
		f.Done(liveconfig.PhaseSetup)
	}()
	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"phase":"setup"`)
	f := <-got
	assert.Equal(t, good, f.Config.Database.Database)
	require.NotNil(t, f.DB)
	if sqlDB, err := f.DB.DB(); err == nil {
		_ = sqlDB.Close()
	}

	st, err := os.Stat(store.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	kept, ok, err := store.Load()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, good, kept.Database)

	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusConflict, w.Code, "a second fix is refused")
}

// TestSetupWizardLeavesALockedDatabaseToTheDeployment starts the wizard of
// a hub whose environment sets the database host. The wizard cannot change
// the database and says so.
func TestSetupWizardLeavesALockedDatabaseToTheDeployment(t *testing.T) {
	t.Parallel()
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	_, r := brokenDatabase(t, liveconfig.Locks{"database.host": {Source: liveconfig.SourceEnv}}, store)
	raw, cfg := wizardGET(t, r)
	assert.True(t, cfg.Wizard.DatabaseLocked)
	assert.False(t, cfg.Settings["database.database"].Editable)
	setDatabase(raw, filepath.Join(t.TempDir(), "DMRHub.db"))
	w := wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "the deployment sets the database")
}

// TestSetupWizardSaysWhenItCannotKeepTheDatabase starts the wizard in a
// working directory that DMRHub cannot write, as on a read-only mount. The
// wizard cannot change the database and says where it would keep it.
func TestSetupWizardSaysWhenItCannotKeepTheDatabase(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "readonly")
	require.NoError(t, os.Mkdir(dir, 0o500))
	store := liveconfig.DatabaseState{Path: filepath.Join(dir, liveconfig.DatabaseStateFile)}
	_, r := brokenDatabase(t, liveconfig.Locks{}, store)
	raw, cfg := wizardGET(t, r)
	assert.Contains(t, cfg.Wizard.StoreError, dir)
	assert.False(t, cfg.Settings["database.database"].Editable)
	setDatabase(raw, filepath.Join(t.TempDir(), "DMRHub.db"))
	w := wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "cannot keep a database setting")
}

// TestSetupWizardListensOnTheConfiguredAddress makes the setup wizard
// listen on http.bind and http.port, as the hub does after setup.
func TestSetupWizardListensOnTheConfiguredAddress(t *testing.T) {
	t.Parallel()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.HTTP.Bind = "127.0.0.1"
	base.HTTP.Port = 3018
	s := internalhttp.MakeSetupWizardServer(&base, setupwizard.NewState(), make(chan any, 1), "test", "test")
	assert.Equal(t, "127.0.0.1:3018", s.Addr)
}

func wizardCall(t *testing.T, r *gin.Engine, method, path string, body any, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func wizardStatus(t *testing.T, r *gin.Engine) (open, valid bool) {
	t.Helper()
	w := wizardCall(t, r, http.MethodGet, "/api/v1/setupwizard", nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		Open  bool `json:"first_admin_open"`
		Valid bool `json:"config_valid"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	return got.Open, got.Valid
}

// TestSetupWizardLetsTheFirstVisitorMakeTheAdmin runs the setup wizard on
// two replicas of one hub with complete settings and no admin. Visitors of
// both send the first admin at once, with no token. Exactly one becomes
// the admin and the others get 409. The wizard ends only once an admin
// exists, and refuses a request that a page of another site sends.
func TestSetupWizardLetsTheFirstVisitorMakeTheAdmin(t *testing.T) {
	t.Parallel()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "DMRHub.db")
	base.Secret = "a-secret-that-is-long-enough-for-the-check"
	base.PasswordSalt = "a-salt-that-is-long-enough-for-the-check"
	base.HTTP.CanonicalHost = "http://example.com"
	base.DMR.DisableRadioIDValidation = true

	var routers [2]*gin.Engine
	var done [2]chan any
	for i := range routers {
		database, err := db.MakeDB(&base)
		require.NoError(t, err)
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		live := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
		require.NoError(t, live.Boot(context.Background()))
		require.NoError(t, live.Current().Get().Validate())
		state := setupwizard.NewState()
		state.SetLive(live, database)
		done[i] = make(chan any, 1)
		routers[i] = internalhttp.CreateSetupWizardRouter(&base, state, done[i], "test", "test")
	}

	open, valid := wizardStatus(t, routers[0])
	assert.True(t, open)
	assert.True(t, valid)
	w := wizardCall(t, routers[0], http.MethodPost, "/api/v1/setupwizard/complete", nil, "")
	assert.Equal(t, http.StatusConflict, w.Code, "the wizard does not end without an admin: %s", w.Body.String())

	user := func(i int) map[string]any {
		return map[string]any{"id": 3000001 + i, "callsign": fmt.Sprintf("N%dCALL", i), "username": fmt.Sprintf("admin%d", i), "password": "Correct-Horse-Battery-9"}
	}
	w = wizardCall(t, routers[0], http.MethodPost, "/api/v1/users", user(9), "http://evil.example")
	assert.Equal(t, http.StatusForbidden, w.Code, "a page of another site cannot make the admin: %s", w.Body.String())

	const visitors = 6
	codes := make([]int, visitors)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range visitors {
		wg.Go(func() {
			<-start
			codes[i] = wizardCall(t, routers[i%2], http.MethodPost, "/api/v1/users", user(i), "http://example.com").Code
		})
	}
	close(start)
	wg.Wait()
	won := 0
	for i, code := range codes {
		if code == http.StatusOK {
			won++
			continue
		}
		assert.Equal(t, http.StatusConflict, code, "visitor %d", i)
	}
	assert.Equal(t, 1, won, "visitors that became the admin: %v", codes)

	for i, r := range routers {
		open, _ := wizardStatus(t, r)
		assert.False(t, open, "replica %d", i)
		w := wizardCall(t, r, http.MethodPost, "/api/v1/setupwizard/complete", nil, "")
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		select {
		case <-done[i]:
		default:
			t.Errorf("replica %d did not end its setup wizard", i)
		}
	}
}

// TestSetupWizardMakesTheFirstAdminQuietly makes the first admin through
// the setup wizard, which serves no websockets, and logs no error.
func TestSetupWizardMakesTheFirstAdminQuietly(t *testing.T) { //nolint:paralleltest // it swaps the default logger
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&base)
	base.Secret = "a-secret-that-is-long-enough-for-the-check"
	base.PasswordSalt = "a-salt-that-is-long-enough-for-the-check"
	base.DMR.DisableRadioIDValidation = true
	database, err := db.MakeDB(&base)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	live := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
	require.NoError(t, live.Boot(context.Background()))
	state := setupwizard.NewState()
	state.SetLive(live, database)
	r := internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var body bytes.Buffer
	require.NoError(t, json.NewEncoder(&body).Encode(map[string]any{
		"id": 3000001, "callsign": "N0DEMO", "username": "admin", "password": "a-long-enough-password",
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/users", &body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var admin models.User
	require.NoError(t, database.First(&admin, 3000001).Error)
	assert.True(t, admin.SuperAdmin)
	assert.Empty(t, logs.String())
}

func wizardClosed(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := wizardCall(t, r, http.MethodGet, "/api/v1/setupwizard", nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		Closed string `json:"closed"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	return got.Closed
}

// TestSetupWizardKeepsASavedDatabase starts DMRHub as bootConfig does when
// the state file keeps a database setting that DMRHub cannot reach, as
// when the database of a running hub goes down. A visitor cannot read the
// settings or point DMRHub at another database, and the wizard says to fix
// the config and restart.
func TestSetupWizardKeepsASavedDatabase(t *testing.T) {
	t.Parallel()
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	saved := base.Database
	saved.Database = filepath.Join(t.TempDir(), "gone", "DMRHub.db")
	require.NoError(t, store.Save(saved))
	kept, ok, err := store.Load()
	require.NoError(t, err)
	require.True(t, ok)
	base.Database = kept
	_, openErr := db.MakeDB(&base)
	require.Error(t, openErr)
	fix := liveconfig.NewDatabaseFix(&base, liveconfig.Locks{}, store, db.MakeDB, openErr)
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	r := internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test")

	got := make(chan liveconfig.Fixed, 1)
	go func() {
		f, ok := <-fix.Fixed()
		if !ok {
			return
		}
		got <- f
		f.Done(liveconfig.PhaseSetup)
	}()

	assert.Equal(t, "database", wizardClosed(t, r))
	w := wizardRequest(t, r, http.MethodGet, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(mustJSON(t, base), &raw))
	setDatabase(raw, filepath.Join(t.TempDir(), "theirs.db"))
	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "restart DMRHub")

	select {
	case f := <-got:
		if sqlDB, err := f.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		t.Fatalf("the wizard switched DMRHub to %s", f.Config.Database.Database)
	default:
	}
	kept, ok, err = store.Load()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, saved.Database, kept.Database)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// TestSetupWizardKeepsTheSettingsOfAnAdmin starts the setup wizard of a hub
// that has an admin but settings that are not valid, as after an upgrade
// that needs a new setting. A visitor cannot read or change the settings,
// and the wizard says to fix the config and restart.
func TestSetupWizardKeepsTheSettingsOfAnAdmin(t *testing.T) {
	t.Parallel()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "DMRHub.db")
	base.Secret = "a-secret-that-is-long-enough-for-the-check"
	base.PasswordSalt = "a-salt-that-is-long-enough-for-the-check"
	base.DMR.DisableRadioIDValidation = true
	database, err := db.MakeDB(&base)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, models.ClaimFirstAdmin(database, &models.User{ID: 3000001, Callsign: "N0ADM", Username: "admin", Password: "x"}))
	live := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
	require.NoError(t, live.Boot(context.Background()))
	require.Error(t, live.Current().Get().Validate())
	state := setupwizard.NewState()
	state.SetLive(live, database)
	r := internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test")

	assert.Equal(t, "settings", wizardClosed(t, r))
	w := wizardRequest(t, r, http.MethodGet, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(mustJSON(t, base), &raw))
	delete(raw, "secret")
	delete(raw, "password-salt")
	httpSection, ok := raw["http"].(map[string]any)
	require.True(t, ok)
	httpSection["canonical-host"] = "https://theirs.example"
	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "restart DMRHub")
	assert.Empty(t, live.Current().Get().HTTP.CanonicalHost)
}

// sqliteHub returns a default config whose SQLite file DMRHub made, with n
// users in it, and that DMRHub can no longer use, and the database fix of
// its start.
func sqliteHub(t *testing.T, n int, store liveconfig.DatabaseState) (*config.Config, *liveconfig.DatabaseFix) {
	t.Helper()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "DMRHub.db")
	database, err := db.MakeDB(&base)
	require.NoError(t, err)
	for i := range n {
		require.NoError(t, database.Create(&models.User{ID: uint(3000001 + i), Callsign: fmt.Sprintf("N0US%c", 'A'+i), Username: fmt.Sprintf("user%d", i), Password: "x"}).Error)
	}
	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	base.Database.ExtraParameters = append(base.Database.ExtraParameters, "_txlock=broken")
	_, openErr := db.MakeDB(&base)
	require.Error(t, openErr)
	return &base, liveconfig.NewDatabaseFix(&base, liveconfig.Locks{}, store, db.MakeDB, openErr)
}

// TestSetupWizardKeepsASQLiteFileWithUsers starts the wizard of a default
// install whose SQLite file has a user but fails, with no state file. The
// file counts as a saved database setting: a visitor cannot point DMRHub at
// another database, and the wizard says to fix the config and restart.
func TestSetupWizardKeepsASQLiteFileWithUsers(t *testing.T) {
	t.Parallel()
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	base, fix := sqliteHub(t, 1, store)
	assert.True(t, fix.Closed())
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	r := internalhttp.CreateSetupWizardRouter(base, state, make(chan any, 1), "test", "test")

	assert.Equal(t, "database", wizardClosed(t, r))
	w := wizardRequest(t, r, http.MethodGet, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(mustJSON(t, base), &raw))
	setDatabase(raw, filepath.Join(t.TempDir(), "theirs.db"))
	w = wizardRequest(t, r, http.MethodPut, raw)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "restart DMRHub")
	assert.False(t, store.Saved())
}

// TestSetupWizardFixesASQLiteFileWithNoUsers starts the wizard of a first
// install whose SQLite file has no user and fails. The wizard offers the
// database step.
func TestSetupWizardFixesASQLiteFileWithNoUsers(t *testing.T) {
	t.Parallel()
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	base, fix := sqliteHub(t, 0, store)
	assert.False(t, fix.Closed())
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	r := internalhttp.CreateSetupWizardRouter(base, state, make(chan any, 1), "test", "test")

	assert.Empty(t, wizardClosed(t, r))
	_, cfg := wizardGET(t, r)
	require.NotNil(t, cfg.Wizard)
	assert.Equal(t, liveconfig.PhaseDatabase, cfg.Wizard.Phase)
}
