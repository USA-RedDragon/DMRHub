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

package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const readTimeout = 3 * time.Second

func CreateMetricsServer(config *config.Config) {
	if config.Metrics.Enabled {
		http.Handle("/metrics", promhttp.Handler())
		server := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", config.Metrics.Bind, config.Metrics.Port),
			ReadHeaderTimeout: readTimeout,
		}
		err := server.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}
}
