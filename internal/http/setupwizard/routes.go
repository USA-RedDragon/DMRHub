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

package setupwizard

import (
	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	v1APIControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1"
	v1APIConfigControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	v1SetupWizardControllers "github.com/USA-RedDragon/DMRHub/internal/http/setupwizard/controllers/v1/setupwizard"
	"github.com/gin-gonic/gin"
)

// ApplyRoutes to the HTTP Mux.
func ApplyRoutes(config *configPkg.Config, router *gin.Engine) {
	apiV1 := router.Group("/api/v1")
	v1(apiV1)
}

func v1(group *gin.RouterGroup) {
	group.PUT("/config", v1APIConfigControllers.PUTConfig)
	group.GET("/config", v1APIConfigControllers.GETConfig)
	group.GET("/config/validate", v1APIConfigControllers.GETConfigValidate)
	group.POST("/config/validate", v1APIConfigControllers.POSTConfigValidate)

	group.GET("/setupwizard", v1SetupWizardControllers.GETSetupWizard)
	group.GET("/network/name", v1APIControllers.GETNetworkName)
	group.GET("/version", v1APIControllers.GETVersion)
	group.GET("/ping", v1APIControllers.GETPing)
}
