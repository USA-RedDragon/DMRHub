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

package users

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-gonic/gin"
)

func firstID(p *models.Plugin) uint {
	first := uint(0)
	for _, r := range p.Ranges {
		if first == 0 || r.First < first {
			first = r.First
		}
	}
	return first
}

func pluginSwitchOf(u *models.UserPluginSwitch) apimodels.PluginSwitch {
	id := ""
	if first := firstID(&u.Plugin); first != 0 {
		id = strconv.FormatUint(uint64(first), 10)
	}
	fill := strings.NewReplacer("{callsign}", u.Callsign, "{choice}", u.Choice, "{id}", id)
	out := apimodels.PluginSwitch{
		PluginID: u.Plugin.ID, Plugin: u.Plugin.Name, Key: u.Spec.Key, Title: u.Spec.Title,
		Description: u.Spec.Description, Notice: u.Spec.Notice, On: u.On, Details: []apimodels.PluginSwitchDetail{},
	}
	for _, d := range u.Spec.Details {
		out.Details = append(out.Details, apimodels.PluginSwitchDetail{Label: d.Label, Value: fill.Replace(d.Value)})
	}
	if c := u.Spec.Choice; c != nil {
		out.Choice = &apimodels.PluginSwitchChoice{Label: c.Label, Options: c.Options, Value: u.Choice}
	}
	return out
}

// GETMyPluginSwitches lists the switches of the plugins that are on, with
// how the logged-in user set them.
func GETMyPluginSwitches(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	list, err := models.ListUserPluginSwitches(db, &user)
	if err != nil {
		slog.Error("Error listing plugin switches", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	out := make([]apimodels.PluginSwitch, 0, len(list))
	for i := range list {
		out = append(out, pluginSwitchOf(&list[i]))
	}
	c.JSON(http.StatusOK, out)
}

// PUTMyPluginSwitch turns a switch of a plugin on or off for the logged-in
// user, with a choice, and tells the plugin.
func PUTMyPluginSwitch(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	var req apimodels.PluginSwitchPut
	if err := c.ShouldBindJSON(&req); err != nil || req.On == nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Send on as true or false"})
		return
	}
	id, err := strconv.ParseUint(c.Param("plugin"), 10, 32)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "No such switch"})
		return
	}
	p, err := models.FindPlugin(db, uint(id))
	switch {
	case errors.Is(err, models.ErrPluginNotFound) || (err == nil && !p.Enabled):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "No such switch"})
		return
	case err != nil:
		slog.Error("Error finding a plugin", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	set, err := models.SetUserPluginSwitch(db, p.ID, user.ID, c.Param("key"), *req.On, req.Choice, time.Now())
	switch {
	case errors.Is(err, models.ErrPluginSwitch):
		c.JSON(http.StatusNotFound, gin.H{errorKey: "No such switch or option"})
		return
	case err != nil:
		slog.Error("Error setting a plugin switch", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	if ps, ok := c.Value("PubSub").(pubsub.PubSub); ok {
		if err := plugins.PublishSwitch(ps, p.ID, &set); err != nil {
			slog.Error("Error telling a plugin of a switch", "plugin", p.ID, "error", err)
		}
	} else {
		slog.Error("PubSub not found in context, the plugin does not hear of a switch", "plugin", p.ID)
	}
	set.Plugin = p
	c.JSON(http.StatusOK, pluginSwitchOf(&set))
}
