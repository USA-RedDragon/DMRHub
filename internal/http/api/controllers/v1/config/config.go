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
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/gin-gonic/gin"
)

const (
	errorKey  = "error"
	errorsKey = "errors"
)

// LiveKey is the key of the *liveconfig.Live in the gin context.
const LiveKey = "LiveConfig"

func liveOf(c *gin.Context) (*liveconfig.Live, bool) {
	v, ok := c.Get(LiveKey)
	l, isLive := v.(*liveconfig.Live)
	if !ok || !isLive || l == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: "Setup cannot save settings while the database is not reachable"})
		return nil, false
	}
	return l, true
}

// PUTConfig saves the settings that Setup changes in the database and puts
// them into effect on every replica. It refuses a change of a setting that
// the deployment sets or that takes effect at start only. It answers 409
// when a replica could not put the saved settings into effect, with the
// report of each replica.
func PUTConfig(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	live, ok := liveOf(c)
	if !ok {
		return
	}
	next := &config.Config{}
	req.ToConfig(next, live.Current().Get())

	res, errs, err := live.Save(c.Request.Context(), next)
	switch {
	case len(errs) > 0:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid configuration", errorsKey: FormatErrors(errs)})
		return
	case errors.Is(err, liveconfig.ErrBusy):
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: err.Error()})
		return
	case err != nil:
		slog.Error("Failed to save the Setup settings", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Failed to save the settings"})
		return
	}

	replicas := make([]apimodels.ConfigSaveReplica, 0, len(res.Replicas))
	applyErrs := []apimodels.ConfigApplyError{}
	for _, r := range res.Replicas {
		replicas = append(replicas, apimodels.ConfigSaveReplica{
			ID: r.Replica, Self: r.Self, Answered: r.Answered, Revision: r.Revision,
			ApplyErrors: r.ApplyErrors, Invalid: r.Invalid,
		})
		if !r.Answered {
			continue
		}
		for _, e := range r.ApplyErrors {
			applyErrs = append(applyErrs, apimodels.ConfigApplyError{Replica: r.Replica, Server: e.Server, Running: e.Running, Error: e.Error})
		}
		for _, e := range r.Invalid {
			applyErrs = append(applyErrs, apimodels.ConfigApplyError{Replica: r.Replica, Running: true, Field: e.Field, Error: e.Error})
		}
	}
	out := apimodels.ConfigSaveResponse{Revision: res.Revision, Replicas: replicas, ApplyErrors: applyErrs}
	if res.Failed() {
		out.Error = "Some settings could not be applied"
		c.JSON(http.StatusConflict, out)
		return
	}
	out.Message = "Configuration updated successfully"
	c.JSON(http.StatusOK, out)
}

// GETConfig returns the config that this replica runs with, without its
// secrets, with how each setting stands on this replica and the others.
func GETConfig(c *gin.Context) {
	live, ok := liveOf(c)
	if !ok {
		return
	}
	running := live.Current().Get()
	status, err := live.Status(c.Request.Context())
	if err != nil {
		slog.Error("Failed to read the state of the Setup settings", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}

	response := apimodels.ConfigResponse{
		Config: *running,
		Secrets: apimodels.SecretStatus{
			SecretSet:       running.Secret != "",
			PasswordSaltSet: running.PasswordSalt != "",
			SMTPPasswordSet: running.SMTP.Password != "",
		},
		Status: status,
	}

	c.JSON(http.StatusOK, response)
}

func GETConfigValidate(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	errs := config.ValidateWithFields()

	c.JSON(http.StatusOK, gin.H{"valid": len(errs) == 0, errorsKey: FormatErrors(errs)})
}

// POSTConfigValidate checks a config as PUTConfig does, without a save.
func POSTConfigValidate(c *gin.Context) {
	var req apimodels.POSTConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
		return
	}
	live, ok := liveOf(c)
	if !ok {
		return
	}

	nextCfg := &config.Config{}
	req.ToConfig(nextCfg, live.Current().Get())

	errs := live.Check(nextCfg)

	c.JSON(http.StatusOK, gin.H{"valid": len(errs) == 0, errorsKey: FormatErrors(errs)})
}

// FormatErrors nests the errors of errs by the parts of their fields, as
// the Setup page shows them.
func FormatErrors(errs []config.ValidationError) map[string]any {
	errors := make(map[string]any)

	for _, e := range errs {
		// Field is a string, but may contain dots for nested fields
		// e.g. "smtp.password"
		// We need to split by the dot and create sub-maps as needed
		parts := strings.Split(e.Field, ".")
		currentMap := errors
		for i, part := range parts {
			if i == len(parts)-1 {
				// Last part, set the error message
				currentMap[part] = e.Err.Error()
			} else {
				// Not last part, create a new map if it doesn't exist
				if _, ok := currentMap[part]; !ok {
					currentMap[part] = make(map[string]any)
				}
				// Move to the next map
				nextMap, ok := currentMap[part].(map[string]any)
				if !ok {
					// This should never happen, but just in case
					break
				}
				currentMap = nextMap
			}
		}

		// Set the error message for the last part
		currentMap[parts[len(parts)-1]] = e.Err.Error()
	}

	return errors
}

// TranscodingStatus is the state of the AMBE servers that convert calls
// between Codec2 and AMBE, in order; none when transcoding is off.
type TranscodingStatus struct {
	Servers []AMBEServerStatus `json:"servers"`
}

// AMBEServerStatus is the state of one AMBE server.
type AMBEServerStatus struct {
	Server string `json:"server"`
	// CallsLimit is the calls the server converts at once, 0 for no limit.
	CallsLimit int `json:"calls_limit"`
	// AnsweredSeconds is how many seconds ago the server last answered,
	// nil when it has not answered within answeredWithin.
	AnsweredSeconds *int64 `json:"answered_seconds"`
	// Calls is the number of calls that hold the server now.
	Calls int `json:"calls"`
}

// answeredWithin is how recent the last answer of the AMBE server must be
// for the status to show it. The hub asks every transcode.ProbeInterval.
const answeredWithin = 30 * time.Second

// GETTranscoding returns the TranscodingStatus. The calls are those of
// every replica; the answers are those that this replica saw.
func GETTranscoding(c *gin.Context) {
	h, ok := c.Get("Hub")
	dmrHub, isHub := h.(*hub.Hub)
	if !ok || !isHub || dmrHub == nil {
		slog.Error("Hub not found in context")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}
	status := TranscodingStatus{Servers: []AMBEServerStatus{}}
	for _, st := range dmrHub.Transcoder().Status(c.Request.Context()) {
		s := AMBEServerStatus{Server: st.Addr, CallsLimit: st.Calls, Calls: st.CallsNow}
		if ago := time.Since(st.AnsweredAt); st.Answered && ago < answeredWithin {
			secs := int64(ago / time.Second)
			s.AnsweredSeconds = &secs
		}
		status.Servers = append(status.Servers, s)
	}
	c.JSON(http.StatusOK, status)
}
