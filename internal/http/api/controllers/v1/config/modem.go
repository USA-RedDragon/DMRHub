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
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/gin-gonic/gin"
)

// ModemKey is the key of the *Modem in the gin context.
const ModemKey = "Modem"

// modemTestTimeout bounds a test of the modem: the wait after the port
// opens, the version tries and the two commands, with room to spare.
const modemTestTimeout = 20 * time.Second

// ModemHost is the modem server of this replica.
type ModemHost interface {
	// ModemStatus returns the live state of the modem server, and false
	// when it does not run.
	ModemStatus() (modem.Status, bool)
}

// Modem is what the modem API needs.
type Modem struct {
	Host ModemHost
	// KV holds the live states of the modems of the other replicas.
	KV      kv.KV
	Replica string
	// Probe is the options of a test; the zero value means
	// modem.DefaultOptions.
	Probe modem.Options
	// Ports lists the serial ports; nil means modem.SerialPorts.
	Ports func() []modem.SerialPort

	testing sync.Mutex
}

// ModemState is the live state of the modem, from the replica that drives
// it.
type ModemState struct {
	// Status is nil when no replica runs the modem.
	Status *modem.Status `json:"status"`
	// Self is whether the status is that of this replica.
	Self bool `json:"self"`
}

// ModemTest is the answer to a test of the modem.
type ModemTest struct {
	// Live is true when the modem runs on the port, and the answer is its
	// live state instead of a test.
	Live   bool               `json:"live"`
	State  *ModemState        `json:"state,omitempty"`
	Result *modem.ProbeResult `json:"result,omitempty"`
}

func modemOf(c *gin.Context) (*Modem, bool) {
	v, ok := c.Get(ModemKey)
	m, isModem := v.(*Modem)
	if !ok || !isModem || m == nil {
		slog.Error("Modem API not found in context")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: tryAgain})
		return nil, false
	}
	return m, true
}

// state returns the live state of the modem: that of this replica when it
// runs the modem, else that of a replica whose link is up, else that of
// any replica that runs it.
func (m *Modem) state(ctx context.Context) ModemState {
	if m.Host != nil {
		if st, ok := m.Host.ModemStatus(); ok && st.Linked {
			return ModemState{Status: &st, Self: true}
		}
	}
	var down *modem.Status
	for _, st := range modem.Statuses(ctx, m.KV) {
		if st.Replica == m.Replica {
			continue
		}
		if st.Linked {
			return ModemState{Status: &st}
		}
		if down == nil {
			down = &st
		}
	}
	if m.Host != nil {
		if st, ok := m.Host.ModemStatus(); ok {
			return ModemState{Status: &st, Self: true}
		}
	}
	return ModemState{Status: down}
}

// holder returns the live state of the modem that holds port: the modem
// server of this replica, or a replica whose link to a board on a port of
// that name is up. It returns nil when none does.
func (m *Modem) holder(ctx context.Context, port string) *ModemState {
	if m.Host != nil {
		if st, ok := m.Host.ModemStatus(); ok && samePort(st.Port, port) {
			return &ModemState{Status: &st, Self: true}
		}
	}
	for _, st := range modem.Statuses(ctx, m.KV) {
		if st.Replica != m.Replica && st.Linked && st.Port == port {
			return &ModemState{Status: &st}
		}
	}
	return nil
}

// GETModem returns the ModemState.
func GETModem(c *gin.Context) {
	m, ok := modemOf(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, m.state(c.Request.Context()))
}

// GETModemPorts returns the serial ports of this replica.
func GETModemPorts(c *gin.Context) {
	m, ok := modemOf(c)
	if !ok {
		return
	}
	list := modem.SerialPorts
	if m.Ports != nil {
		list = m.Ports
	}
	c.JSON(http.StatusOK, gin.H{"ports": list()})
}

func samePort(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
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
// fields of dmr.modem, over the settings that run: it opens the serial
// port, asks the board for its version and sends it the frequencies and
// the config, and saves nothing. When the modem runs on that port, the
// answer is its live state instead. It answers 400 with the errors of the
// settings, and 409 while another test runs.
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
	ctx := c.Request.Context()
	if st := m.holder(ctx, settings.Port); st != nil {
		c.JSON(http.StatusOK, ModemTest{Live: true, State: st})
		return
	}
	if !m.testing.TryLock() {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Another test of the modem runs now"})
		return
	}
	defer m.testing.Unlock()
	opts := m.Probe
	if opts.Open == nil {
		opts = modem.DefaultOptions()
	}
	ctx, cancel := context.WithTimeout(ctx, modemTestTimeout)
	defer cancel()
	r := modem.Probe(ctx, settings, opts)
	c.JSON(http.StatusOK, ModemTest{Result: &r})
}
