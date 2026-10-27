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

package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/gin-gonic/gin"
)

// ModemKey is the key of the *modem.Replicas in the gin context.
const ModemKey = "Modem"

func modemOf(c *gin.Context) (*modem.Replicas, bool) {
	v, ok := c.Get(ModemKey)
	m, isModem := v.(*modem.Replicas)
	if !ok || !isModem || m == nil {
		slog.Error("Modem API not found in context")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: tryAgain})
		return nil, false
	}
	return m, true
}

// GETModem returns the modem.State.
func GETModem(c *gin.Context) {
	m, ok := modemOf(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, m.State(c.Request.Context()))
}

// GETModemPorts returns the serial ports of each replica that answers,
// this one first.
func GETModemPorts(c *gin.Context) {
	m, ok := modemOf(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"replicas": m.AllPorts(c.Request.Context())})
}

// testedField reports whether a test of the modem uses the setting of
// the validation error field.
func testedField(field string) bool {
	for _, p := range []string{"dmr.modem.repeater-id", "dmr.modem.cw-id.", "dmr.modem.beacons."} {
		if strings.HasPrefix(field, p) {
			return false
		}
	}
	return true
}

// POSTModemTest tests the modem settings in the body, which has the
// fields of dmr.modem, over the settings that run, on the replica of the
// port: the one in the replica query parameter, else one that lists the
// port. That replica opens the port, asks the board for its version and
// sends it the frequencies and the config, and saves nothing. When a
// modem runs on that port, the answer is its live state instead. It
// answers 400 with the errors of the settings, and 409 while another test
// runs on that replica.
func POSTModemTest(c *gin.Context) {
	m, ok := modemOf(c)
	if !ok {
		return
	}
	cfg, ok := c.Get("Config")
	current, isConfig := cfg.(*config.Config)
	if !ok || !isConfig || current == nil {
		slog.Error("Config not found in context")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: tryAgain})
		return
	}
	settings := current.DMR.Modem
	if err := json.NewDecoder(c.Request.Body).Decode(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "The body must be the modem settings as JSON"})
		return
	}
	settings.Enabled = true
	var errs []config.ValidationError
	for _, e := range settings.ValidateWithFields() {
		if testedField(e.Field) {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorsKey: FormatErrors(errs)})
		return
	}
	a, err := m.Test(c.Request.Context(), settings, c.Query("replica"))
	if errors.Is(err, modem.ErrTestRuns) {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Another test of the modem runs now"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: tryAgain})
		return
	}
	c.JSON(http.StatusOK, a)
}
