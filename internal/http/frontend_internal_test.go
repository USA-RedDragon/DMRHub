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

package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHandleMimeServesMapAssets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path, contentType, cacheControl string
	}{
		{"/assets/maplibre-gl-worker-abc.mjs", "text/javascript", ""},
		{"/assets/index-abc.js", "text/javascript", ""},
		{"/glyphs/Open Sans Regular/0-255.pbf", "application/x-protobuf", "public, max-age=31536000, immutable"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://hub"+strings.ReplaceAll(tc.path, " ", "%20"), nil)
		handleMime(c, []byte("x"), "frontend/dist"+tc.path)
		assert.Equal(t, tc.contentType, w.Header().Get("Content-Type"), tc.path)
		assert.Equal(t, tc.cacheControl, w.Header().Get("Cache-Control"), tc.path)
	}
}
