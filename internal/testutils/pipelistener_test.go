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

package testutils_test

import (
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAServerOnAPipeListenerAnswersInABubble(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, client := testutils.StartPipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(time.Hour)
			_, _ = io.WriteString(w, "late")
		}))

		start := time.Now()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, "late", string(body))
		assert.Equal(t, time.Hour, time.Since(start), "the hour passes in virtual time")
	})
}

func TestAClosedPipeListenerRefusesDials(t *testing.T) {
	t.Parallel()
	l := testutils.NewPipeListener(t)
	require.NoError(t, l.Close())
	require.NoError(t, l.Close())
	_, err := l.Accept()
	require.Error(t, err)
	_, err = l.DialContext(t.Context(), "tcp", "pipe")
	require.Error(t, err)
}
