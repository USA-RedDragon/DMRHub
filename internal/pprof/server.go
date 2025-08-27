// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package pprof

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

const readTimeout = 3 * time.Second

func CreatePProfServer(config *config.Config) {
	if config.PProf.Enabled {
		r := gin.New()
		r.Use(gin.Logger())
		r.Use(gin.Recovery())

		// Tracing
		if config.Metrics.OTLPEndpoint != "" {
			r.Use(otelgin.Middleware("pprof"))
			r.Use(middleware.TracingProvider(config))
		}

		err := r.SetTrustedProxies(config.PProf.TrustedProxies)
		if err != nil {
			slog.Error("Failed setting trusted proxies", "error", err)
		}

		pprof.Register(r)

		server := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", config.PProf.Bind, config.PProf.Port),
			Handler:           r,
			ReadHeaderTimeout: readTimeout,
		}
		slog.Info("PProf Server Listening", "address", server.Addr)
		err = server.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}
}
