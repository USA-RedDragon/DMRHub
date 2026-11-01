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
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type migrationFunc func(context.Context) (*gorm.DB, error)

func (f migrationFunc) Migrate(ctx context.Context) (*gorm.DB, error) {
	return f(ctx)
}

type wizardFixServer struct {
	fix    *liveconfig.DatabaseFix
	store  liveconfig.DatabaseState
	raw    map[string]any
	server *http.Server
	client *http.Client
}

// serveDatabaseFix starts the HTTP server of the wizard of a hub that
// cannot reach its database, on a PipeListener, and returns it with a
// config whose database DMRHub can reach.
func serveDatabaseFix(t *testing.T) *wizardFixServer {
	t.Helper()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "missing", "DMRHub.db")
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	fix := liveconfig.NewDatabaseFix(&base, liveconfig.Locks{}, store, liveconfig.OpenDatabase, errors.New("down"))
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	s := internalhttp.MakeSetupWizardServer(&base, state, make(chan any, 1), "test", "test")
	ln := testutils.NewPipeListener(t)
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })
	raw, _ := wizardGET(t, internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test"))
	setDatabase(raw, filepath.Join(t.TempDir(), "DMRHub.db"))
	return &wizardFixServer{fix: fix, store: store, raw: raw, server: s.Server, client: ln.Client()}
}

func (w *wizardFixServer) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequestWithContext(t.Context(), method, "http://wizard"+path, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	require.NoError(t, err, "the browser gets no answer")
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "the browser gets no whole answer")
	got := map[string]any{}
	require.NoError(t, json.Unmarshal(b, &got), string(b))
	return resp.StatusCode, got
}

func (w *wizardFixServer) put(t *testing.T, database string) (int, map[string]any) {
	t.Helper()
	raw := w.raw
	if database != "" {
		raw = map[string]any{}
		require.NoError(t, json.Unmarshal(mustJSON(t, w.raw), &raw))
		setDatabase(raw, database)
	}
	return w.call(t, http.MethodPut, "/api/v1/config", raw)
}

func (w *wizardFixServer) status(t *testing.T) map[string]any {
	t.Helper()
	code, got := w.call(t, http.MethodGet, "/api/v1/setupwizard/database-fix", nil)
	require.Equal(t, http.StatusOK, code, got)
	return got
}

func closeGorm(t *testing.T, database *gorm.DB) {
	t.Helper()
	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// TestSetupWizardFinishesAMigrationThatOutlastsTheServer fixes the
// database through the HTTP server of the wizard with a migration that
// takes longer than the read and write timeouts of the server. The PUT
// answers at once, the migration runs to its end after the request ends,
// and the status of the fix then reports the phase that comes next.
func TestSetupWizardFinishesAMigrationThatOutlastsTheServer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := serveDatabaseFix(t)
		migration := 2 * max(w.server.ReadTimeout, w.server.WriteTimeout, liveconfig.FixTimeout)
		ended := make(chan error, 1)
		w.fix.Open = func(ctx context.Context, cfg *config.Config) (liveconfig.Migration, error) {
			m, err := liveconfig.OpenDatabase(ctx, cfg)
			if err != nil {
				return nil, fmt.Errorf("open: %w", err)
			}
			return migrationFunc(func(ctx context.Context) (*gorm.DB, error) {
				time.Sleep(migration)
				ended <- ctx.Err()
				return m.Migrate(ctx)
			}), nil
		}
		took := make(chan liveconfig.Fixed, 1)
		go func() {
			f := <-w.fix.Fixed()
			took <- f
			f.Done(liveconfig.PhaseSetup)
		}()

		start := time.Now()
		code, got := w.put(t, "")
		require.Equal(t, http.StatusAccepted, code, got)
		assert.Equal(t, "pending", got["status"])
		assert.Zero(t, time.Since(start), "the PUT waits for the migration")
		assert.Equal(t, "pending", w.status(t)["status"])

		for got = w.status(t); got["status"] == "pending"; got = w.status(t) {
			time.Sleep(time.Second)
		}
		assert.Equal(t, "done", got["status"], got)
		assert.Equal(t, liveconfig.PhaseSetup, got["phase"])
		assert.GreaterOrEqual(t, time.Since(start), migration)
		require.NoError(t, <-ended, "the end of the request ended the context of the migration")
		f := <-took
		closeGorm(t, f.DB)
		kept, ok, err := w.store.Load()
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, f.Config.Database.Database, kept.Database)
	})
}

// TestSetupWizardAnswersAConnectionThatHangs fixes the database through
// the HTTP server of the wizard with a connection that never ends. The
// browser gets the answer that the fix gave up after FixTimeout, before
// the server cuts the connection, and the status of the fix says why.
func TestSetupWizardAnswersAConnectionThatHangs(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := serveDatabaseFix(t)
		require.Less(t, liveconfig.FixTimeout, min(w.server.ReadTimeout, w.server.WriteTimeout), "a fix must answer before the server cuts the connection")
		w.fix.Open = func(ctx context.Context, _ *config.Config) (liveconfig.Migration, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		start := time.Now()
		code, got := w.put(t, "")
		require.Equal(t, http.StatusBadRequest, code, got)
		assert.Contains(t, got["error"], "cannot reach this database")
		assert.Equal(t, liveconfig.FixTimeout, time.Since(start))
		got = w.status(t)
		assert.Equal(t, "failed", got["status"])
		assert.Contains(t, got["error"], "cannot reach this database")
		assert.False(t, w.store.Saved(), "a fix that gives up keeps no setting")
	})
}

// TestSetupWizardAnswersASecondFixWithTheFirst sends a second fix while the
// migration of the first runs. The second gets the status of the first,
// and only the first database is tried.
func TestSetupWizardAnswersASecondFixWithTheFirst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := serveDatabaseFix(t)
		var opens atomic.Int32
		release := make(chan struct{})
		w.fix.Open = func(ctx context.Context, cfg *config.Config) (liveconfig.Migration, error) {
			opens.Add(1)
			m, err := liveconfig.OpenDatabase(ctx, cfg)
			if err != nil {
				return nil, fmt.Errorf("open: %w", err)
			}
			return migrationFunc(func(ctx context.Context) (*gorm.DB, error) {
				<-release
				return m.Migrate(ctx)
			}), nil
		}
		took := make(chan liveconfig.Fixed, 1)
		go func() {
			f := <-w.fix.Fixed()
			took <- f
			f.Done(liveconfig.PhaseDone)
		}()

		code, got := w.put(t, "")
		require.Equal(t, http.StatusAccepted, code, got)
		code, got = w.put(t, filepath.Join(t.TempDir(), "other.db"))
		require.Equal(t, http.StatusAccepted, code, got)
		assert.Equal(t, "pending", got["status"])
		assert.Equal(t, int32(1), opens.Load(), "the second fix tried its database")

		close(release)
		synctest.Wait()
		got = w.status(t)
		assert.Equal(t, "done", got["status"], got)
		assert.Equal(t, liveconfig.PhaseDone, got["phase"])
		f := <-took
		closeGorm(t, f.DB)
		first, ok := w.raw["database"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, first["database"], f.Config.Database.Database)
		assert.Equal(t, int32(1), opens.Load())
	})
}

// TestSetupWizardClosesADatabaseThatNothingTakes fixes the database while
// nothing takes the database from the fix. After HandOffTimeout the status
// of the fix reports that the start did not take it, and the database is
// closed.
func TestSetupWizardClosesADatabaseThatNothingTakes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := serveDatabaseFix(t)
		migrated := make(chan *gorm.DB, 1)
		w.fix.Open = func(ctx context.Context, cfg *config.Config) (liveconfig.Migration, error) {
			m, err := liveconfig.OpenDatabase(ctx, cfg)
			if err != nil {
				return nil, fmt.Errorf("open: %w", err)
			}
			return migrationFunc(func(ctx context.Context) (*gorm.DB, error) {
				database, err := m.Migrate(ctx)
				migrated <- database
				if err != nil {
					return nil, fmt.Errorf("migrate: %w", err)
				}
				return database, nil
			}), nil
		}

		code, got := w.put(t, "")
		require.Equal(t, http.StatusAccepted, code, got)
		database := <-migrated
		require.NotNil(t, database)
		synctest.Wait()
		assert.Equal(t, "pending", w.status(t)["status"])

		time.Sleep(liveconfig.HandOffTimeout)
		synctest.Wait()
		got = w.status(t)
		assert.Equal(t, "failed", got["status"])
		assert.Contains(t, got["error"], "did not take the database")
		sqlDB, err := database.DB()
		require.NoError(t, err)
		assert.ErrorContains(t, sqlDB.PingContext(t.Context()), "database is closed")
	})
}
