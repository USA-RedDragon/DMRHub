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

package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EngineKey is the key of the gin context that holds the *wasm.Engine that
// inspects uploaded plugins.
const EngineKey = "PluginEngine"

const (
	// maxUpload is the largest plugin file an admin may upload. The whole
	// form stays in memory, as the Docker image has no directory for
	// temporary files.
	maxUpload = 32 << 20
	// maxForm is the largest form of an upload: the file and its fields.
	maxForm = maxUpload + 1<<20
	// uploadTime is how long an upload, and the check of its plugin, may
	// take.
	uploadTime = 5 * time.Minute
	// inspectTime is how long the check of an uploaded plugin may take,
	// which compiles it.
	inspectTime = 2 * time.Minute

	msgNotAPlugin = "Choose a .wasm plugin file."
	msgNoRemove   = "A built-in plugin cannot be removed. Turn it off instead."
	msgNoToken    = "Only a plugin that runs as its own program has a token." //nolint:gosec // a message, not a credential
	msgNoVersion  = "Only an uploaded plugin takes a new version."
	msgBadForm    = "Send the plugin file, its IDs and the hosts it may reach."
)

func engineOf(c *gin.Context) *wasm.Engine {
	if e, ok := c.Get(EngineKey); ok {
		if engine, ok := e.(*wasm.Engine); ok && engine != nil {
			return engine
		}
	}
	return wasm.NewEngine(wasm.DefaultLimits())
}

// external checks that the plugin id runs as its own program, and answers
// msg when it does not.
func external(c *gin.Context, db *gorm.DB, id uint, msg string) bool {
	p, err := models.FindPlugin(db, id)
	switch {
	case errors.Is(err, models.ErrPluginNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return false
	case err != nil:
		failed(c, "cannot read a plugin", err)
		return false
	case p.Kind != models.PluginExternal:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msg})
		return false
	}
	return true
}

// readModule reads the plugin file of the form, and answers when there is
// none.
func readModule(c *gin.Context) ([]byte, bool) {
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetReadDeadline(time.Now().Add(uploadTime))
	_ = rc.SetWriteDeadline(time.Now().Add(uploadTime))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxForm)
	err := c.Request.ParseMultipartForm(maxForm)
	var fh *multipart.FileHeader
	if err == nil {
		fh, err = c.FormFile("file")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{errorKey: msgNotAPlugin})
			return nil, false
		}
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNotAPlugin})
		return nil, false
	}
	if fh.Size > maxUpload {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{errorKey: msgNotAPlugin})
		return nil, false
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNotAPlugin})
		return nil, false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxUpload))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNotAPlugin})
		return nil, false
	}
	return b, true
}

// infoOf returns what hello says. A version that is too long, such as the
// pseudo-version of a Go module, is cut to fit.
func infoOf(h *pluginv1.Hello) (models.PluginInfo, []models.PluginRange) {
	version := h.GetVersion()
	if len(version) > models.MaxPluginVersion {
		version = version[:models.MaxPluginVersion]
	}
	info := models.PluginInfo{Name: h.GetName(), Version: version, Author: h.GetAuthor(), Description: h.GetDescription()}
	for _, host := range h.GetHosts() {
		info.Hosts = append(info.Hosts, models.PluginHost{Address: host.GetAddress(), Reason: host.GetReason()})
	}
	ranges := make([]models.PluginRange, 0, len(h.GetIds()))
	for _, r := range h.GetIds() {
		ranges = append(ranges, models.PluginRange{First: uint(r.GetFirst()), Last: uint(r.GetLast())})
	}
	return info, ranges
}

// inspect runs module until it says hello, and answers when it is not a
// plugin or says what a plugin may not.
func inspect(c *gin.Context, module []byte) (models.PluginInfo, []models.PluginRange, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), inspectTime)
	defer cancel()
	hello, err := engineOf(c).Inspect(ctx, module)
	if err != nil {
		slog.Info("Plugins: an upload is not a plugin", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNotAPlugin})
		return models.PluginInfo{}, nil, false
	}
	info, ranges := infoOf(hello)
	if err := info.Check(); err != nil {
		slog.Info("Plugins: an upload says what a plugin may not", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNotAPlugin})
		return models.PluginInfo{}, nil, false
	}
	return info, ranges, true
}

// POSTPluginInspect answers what an uploaded plugin file says it is and
// asks for, and adds nothing.
func POSTPluginInspect(c *gin.Context) {
	module, ok := readModule(c)
	if !ok {
		return
	}
	info, ranges, ok := inspect(c, module)
	if !ok {
		return
	}
	out := apimodels.PluginManifest{
		Name: info.Name, Version: info.Version, Author: info.Author, Description: info.Description,
		Ranges: make([]apimodels.PluginRange, len(ranges)), Hosts: make([]apimodels.PluginHost, len(info.Hosts)),
	}
	for i, r := range ranges {
		out.Ranges[i] = apimodels.PluginRange{First: r.First, Last: r.Last}
	}
	for i, h := range info.Hosts {
		out.Hosts[i] = apimodels.PluginHost{Address: h.Address, Reason: h.Reason}
	}
	c.JSON(http.StatusOK, out)
}

// POSTPluginUpload adds an uploaded plugin, from the file of the form, with
// the IDs in its ranges field and the hosts in its allowed_hosts field,
// both JSON. The plugin starts on.
func POSTPluginUpload(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	module, ok := readModule(c)
	if !ok {
		return
	}
	var ranges []apimodels.PluginRange
	var allowed []string
	if err := json.Unmarshal([]byte(c.PostForm("ranges")), &ranges); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadForm})
		return
	}
	if a := c.PostForm("allowed_hosts"); a != "" {
		if err := json.Unmarshal([]byte(a), &allowed); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgBadForm})
			return
		}
	}
	info, _, ok := inspect(c, module)
	if !ok {
		return
	}
	now := time.Now()
	p, err := models.CreateUploadedPlugin(db, info, rangesOf(ranges), allowed, module, now)
	if err != nil {
		if !badInput(c, err) {
			failed(c, "cannot add an uploaded plugin", err)
		}
		return
	}
	changed(c)
	v, err := view(c, db, &p, now)
	if err != nil {
		failed(c, "cannot show a plugin", err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

// POSTPluginModule replaces the module of an uploaded plugin with the file
// of the form, a new version. The hosts that it adds start out not allowed.
func POSTPluginModule(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	p, err := models.FindPlugin(db, id)
	switch {
	case errors.Is(err, models.ErrPluginNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	case err != nil:
		failed(c, "cannot read a plugin", err)
		return
	case p.Kind != models.PluginUploaded:
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgNoVersion})
		return
	}
	module, ok := readModule(c)
	if !ok {
		return
	}
	info, _, ok := inspect(c, module)
	if !ok {
		return
	}
	p, err = models.SetPluginModule(db, id, info, module, time.Now())
	if err != nil {
		if !badInput(c, err) {
			failed(c, "cannot replace the module of a plugin", err)
		}
		return
	}
	changed(c)
	v, err := view(c, db, &p, time.Now())
	if err != nil {
		failed(c, "cannot show a plugin", err)
		return
	}
	c.JSON(http.StatusOK, v)
}
