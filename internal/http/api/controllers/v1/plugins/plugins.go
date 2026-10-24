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

// Package plugins is the admin API of the plugins.
package plugins

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey    = "error"
	msgTryLater = "Try again later"
	msgNotFound = "No such plugin"
)

func failed(c *gin.Context, what string, err error) {
	slog.Error("Plugins: "+what, "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
}

func startOfDay(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

func view(db *gorm.DB, p *models.Plugin, now time.Time) (apimodels.AdminPlugin, error) {
	calls, err := models.CountCallsTo(db, p.Ranges, startOfDay(now))
	if err != nil {
		return apimodels.AdminPlugin{}, fmt.Errorf("could not count the calls of plugin %d: %w", p.ID, err)
	}
	return apimodels.NewAdminPlugin(p, calls, now), nil
}

// connection returns how plugins reach the hub: the host of the canonical
// URL and the plugin port, and the fingerprint of the certificate.
func connection(cfg *config.Config) apimodels.PluginConnection {
	c := cfg.DMR.Plugins
	out := apimodels.PluginConnection{Enabled: c.Enabled, OwnCertificate: c.CertFile == ""}
	host := ""
	if u, err := url.Parse(cfg.HTTP.CanonicalHost); err == nil {
		host = u.Hostname()
	}
	if host != "" {
		out.Address = net.JoinHostPort(host, strconv.Itoa(c.Port))
	}
	if certs, err := plugins.NewCertSource(cfg.GetDerivedSecret(), c.CertFile, c.KeyFile); err == nil {
		out.Fingerprint, _ = certs.Fingerprint()
	}
	return out
}

func changed(c *gin.Context) {
	ps, ok := c.MustGet("PubSub").(pubsub.PubSub)
	if !ok || ps == nil {
		return
	}
	if err := plugins.PublishChanged(ps); err != nil {
		slog.Error("Plugins: cannot announce a change", "error", err)
	}
}

func rangesOf(in []apimodels.PluginRange) []models.PluginRange {
	out := make([]models.PluginRange, len(in))
	for i, r := range in {
		out[i] = models.PluginRange{First: r.First, Last: r.Last}
	}
	return out
}

func badInput(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, models.ErrPluginName):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Pick a name of 1 to 64 characters that no other plugin has"})
	case errors.Is(err, models.ErrPluginRange):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Pick IDs from 1 to 16777215, at most 32 ranges, that overlap neither each other nor another plugin's"})
	case errors.Is(err, models.ErrPluginNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
	default:
		return false
	}
	return true
}

func idParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return 0, false
	}
	return uint(id), true
}

// GETPlugins lists the plugins and how they reach the hub.
func GETPlugins(c *gin.Context) {
	cfg, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	list, err := models.ListPlugins(db)
	if err != nil {
		failed(c, "cannot list the plugins", err)
		return
	}
	now := time.Now()
	out := apimodels.PluginList{Plugins: make([]apimodels.AdminPlugin, 0, len(list))}
	for i := range list {
		v, err := view(db, &list[i], now)
		if err != nil {
			failed(c, "cannot show a plugin", err)
			return
		}
		out.Plugins = append(out.Plugins, v)
	}
	out.Connection = connection(cfg)
	c.JSON(http.StatusOK, out)
}

// POSTPlugin adds a plugin and returns its token, which is shown only now.
func POSTPlugin(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var req apimodels.PluginPost
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Send a name and the ID ranges"})
		return
	}
	now := time.Now()
	p, token, err := models.CreatePlugin(db, req.Name, rangesOf(req.Ranges), now)
	if err != nil {
		if !badInput(c, err) {
			failed(c, "cannot add a plugin", err)
		}
		return
	}
	changed(c)
	v, err := view(db, &p, now)
	if err != nil {
		failed(c, "cannot show a plugin", err)
		return
	}
	c.JSON(http.StatusCreated, apimodels.PluginToken{Plugin: v, Token: token})
}

// PUTPlugin changes the name, the IDs and whether a plugin is on.
func PUTPlugin(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req apimodels.PluginPut
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Send a name, enabled and the ID ranges"})
		return
	}
	p, err := models.UpdatePlugin(db, id, req.Name, *req.Enabled, rangesOf(req.Ranges))
	if err != nil {
		if !badInput(c, err) {
			failed(c, "cannot change a plugin", err)
		}
		return
	}
	changed(c)
	v, err := view(db, &p, time.Now())
	if err != nil {
		failed(c, "cannot show a plugin", err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// POSTPluginToken gives a plugin a new token, which is shown only now. The
// old token stops working and its stream ends.
func POSTPluginToken(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	now := time.Now()
	token, err := models.RotatePluginToken(db, id, now)
	if err != nil {
		if !badInput(c, err) {
			failed(c, "cannot make a plugin token", err)
		}
		return
	}
	changed(c)
	p, err := models.FindPlugin(db, id)
	if err != nil {
		failed(c, "cannot read a plugin", err)
		return
	}
	v, err := view(db, &p, now)
	if err != nil {
		failed(c, "cannot show a plugin", err)
		return
	}
	c.JSON(http.StatusOK, apimodels.PluginToken{Plugin: v, Token: token})
}

// DELETEPlugin removes a plugin and frees its IDs.
func DELETEPlugin(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := models.DeletePlugin(db, id); err != nil {
		if !badInput(c, err) {
			failed(c, "cannot delete a plugin", err)
		}
		return
	}
	changed(c)
	c.Status(http.StatusNoContent)
}
