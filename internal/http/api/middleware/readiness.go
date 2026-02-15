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

package middleware

import (
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// ReadinessProvider injects the readiness flag into the gin context so that
// handlers (e.g. the healthcheck endpoint) can query whether the server is
// fully initialised and ready to accept traffic.
func ReadinessProvider(ready *atomic.Bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("Ready", ready)
		c.Next()
	}
}
