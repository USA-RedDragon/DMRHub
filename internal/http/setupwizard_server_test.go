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
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestSetupWizardAnswersAFixThatHangsBeforeTheServerTimesOut fixes the
// database through the HTTP server of the wizard with a connection that
// never ends. The browser gets the answer that the fix gave up, before the
// read and write timeouts of the server cut the connection.
func TestSetupWizardAnswersAFixThatHangsBeforeTheServerTimesOut(t *testing.T) {
	t.Parallel()
	base, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	base.Database.Database = filepath.Join(t.TempDir(), "missing", "DMRHub.db")
	store := liveconfig.DatabaseState{Path: filepath.Join(t.TempDir(), liveconfig.DatabaseStateFile)}
	fix := liveconfig.NewDatabaseFix(&base, liveconfig.Locks{}, store, func(ctx context.Context, _ *config.Config) (*gorm.DB, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, errors.New("down"))
	state := setupwizard.NewState()
	state.SetDatabaseFix(fix)
	s := internalhttp.MakeSetupWizardServer(&base, state, make(chan any, 1), "test", "test")
	require.Less(t, liveconfig.FixTimeout, min(s.ReadTimeout, s.WriteTimeout), "a fix must end before the server cuts the connection")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })

	raw, _ := wizardGET(t, internalhttp.CreateSetupWizardRouter(&base, state, make(chan any, 1), "test", "test"))
	setDatabase(raw, filepath.Join(t.TempDir(), "DMRHub.db"))
	body, err := json.Marshal(raw)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, "http://"+ln.Addr().String()+"/api/v1/config", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "the browser gets no answer to the fix")
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "the browser gets no whole answer to the fix")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, string(got))
	assert.Contains(t, string(got), "cannot reach this database")
}
