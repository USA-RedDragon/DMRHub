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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wizardToken = "wizard-token"

func wizardRequest(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("X-SetupWizard-Token", wizardToken)
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
	base.Database.Database = fmt.Sprintf("file:wizard_%p", t)
	base.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
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

	r := internalhttp.CreateSetupWizardRouter(&base, live, wizardToken, make(chan any, 1), "test", "test")
	w := wizardRequest(t, r, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	httpSection, ok := cfg["http"].(map[string]any)
	require.True(t, ok)
	httpSection["canonical-host"] = "https://hub.example"

	cfg["secret"] = "another-secret-that-is-long-enough"
	w = wizardRequest(t, r, http.MethodPut, "/api/v1/config", cfg)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "secret")

	delete(cfg, "secret")
	w = wizardRequest(t, r, http.MethodPut, "/api/v1/config", cfg)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, live.Current().Get().Validate())

	next := liveconfig.New(liveconfig.Options{Base: &base, DB: database})
	require.NoError(t, next.Boot(context.Background()))
	assert.Equal(t, "https://hub.example", next.Current().Get().HTTP.CanonicalHost)
}
