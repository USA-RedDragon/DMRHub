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
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
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

// The fields of the upload form that a problem is about.
const (
	fieldFile  = "file"
	fieldIDs   = "ids"
	fieldHosts = "hosts"
)

const (
	msgNotWasm   = "This file is not a WebAssembly module. Choose the .wasm file that the plugin's build made."
	msgNotPlugin = "This file is WebAssembly but not a DMRHub plugin. Plugins are built with GOOS=wasip1 GOARCH=wasm."
)

// uploadProblem is one thing wrong with an upload, and the field of the
// form it is about.
type uploadProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// refuse answers status with problems, the first of them as the error, and
// what the plugin says it is when the hub could read that.
func refuse(c *gin.Context, status int, problems []uploadProblem, manifest *apimodels.PluginManifest) {
	body := gin.H{errorKey: problems[0].Message, "errors": problems}
	if manifest != nil {
		body["manifest"] = manifest
	}
	c.JSON(status, body)
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}

// tooLarge words a file of size bytes that is over maxUpload, or of a size
// the hub does not know when size is 0.
func tooLarge(size int64) uploadProblem {
	most := maxUpload >> 20
	if size <= 0 {
		return uploadProblem{fieldFile, fmt.Sprintf("This file is over %d MiB. A plugin can be at most %d MiB.", most, most)}
	}
	mib := (size + 1<<20 - 1) >> 20
	return uploadProblem{fieldFile, fmt.Sprintf("This file is %d MiB. A plugin can be at most %d MiB.", mib, most)}
}

// startProblem words why the module of an upload is not a plugin that
// starts, within limits l.
func startProblem(err error, l wasm.Limits) uploadProblem {
	var ended *wasm.EndedError
	switch {
	case errors.Is(err, wasm.ErrNotWasm):
		return uploadProblem{fieldFile, msgNotWasm}
	case errors.Is(err, wasm.ErrNotPlugin):
		return uploadProblem{fieldFile, msgNotPlugin}
	case errors.As(err, &ended):
		return uploadProblem{fieldFile, "The plugin stopped before it introduced itself: " + sentence(ended.Message())}
	case errors.Is(err, wasm.ErrNoHello):
		return uploadProblem{fieldFile, fmt.Sprintf("The plugin did not introduce itself within %d seconds. It must call plugin.Main when it starts.",
			int(l.HelloWithin.Round(time.Second)/time.Second))}
	}
	return uploadProblem{fieldFile, "The plugin stopped before it introduced itself: " + sentence(err.Error())}
}

// rangeProblem words what is wrong with the IDs of a plugin, from err of
// models.CheckPluginRanges.
func rangeProblem(err error, asked bool) string {
	var bad *models.PluginIDError
	var taken *models.PluginIDTakenError
	switch {
	case errors.As(err, &taken):
		return fmt.Sprintf("%d is already used by %s. Change the IDs, or turn %s off first.", taken.ID, taken.By, taken.By)
	case errors.As(err, &bad) && asked:
		return fmt.Sprintf("The plugin asks for ID %d, which is not a radio or talkgroup ID.", bad.ID)
	case errors.As(err, &bad):
		return fmt.Sprintf("%d is not a radio or talkgroup ID.", bad.ID)
	}
	return fmt.Sprintf("Use at most %d ranges of IDs that do not overlap each other.", models.MaxPluginRanges)
}

// helloProblems returns what is wrong with what an uploaded plugin says,
// info, and the IDs it would have, ranges. For a new plugin it checks also
// that no other plugin has its name, and that its IDs are free.
func helloProblems(db *gorm.DB, info models.PluginInfo, ranges []models.PluginRange, newPlugin bool) []uploadProblem {
	var out []uploadProblem
	file := func(msg string) { out = append(out, uploadProblem{fieldFile, msg}) }
	switch {
	case info.Name == "" || len(info.Name) > models.MaxPluginName || strings.TrimSpace(info.Name) != info.Name:
		file(fmt.Sprintf("The plugin's name must be 1 to %d characters.", models.MaxPluginName))
	case newPlugin:
		if other, ok, err := models.PluginByName(db, info.Name); err != nil {
			slog.Error("Plugins: cannot check the name of an upload", "error", err)
		} else if ok && other.Kind == models.PluginUploaded {
			file(fmt.Sprintf("%s is already uploaded. Open it and choose “Upload a new version”.", info.Name))
		} else if ok {
			file(fmt.Sprintf("Another plugin is already called %s.", info.Name))
		}
	}
	if info.Version == "" {
		file("The plugin gives no version.")
	}
	if len(info.Author) > models.MaxPluginAuthor {
		file(fmt.Sprintf("The plugin's author must be at most %d characters.", models.MaxPluginAuthor))
	}
	if len(info.Description) > models.MaxPluginDescription {
		file(fmt.Sprintf("The plugin's description must be at most %d characters.", models.MaxPluginDescription))
	}
	if newPlugin {
		if err := models.CheckPluginRanges(db, 0, ranges, true); err != nil {
			if !errors.Is(err, models.ErrPluginRange) {
				slog.Error("Plugins: cannot check the IDs of an upload", "error", err)
			} else {
				out = append(out, uploadProblem{fieldIDs, rangeProblem(err, true)})
			}
		}
	}
	return append(out, hostProblems(info.Hosts)...)
}

func hostProblems(hosts []models.PluginHost) []uploadProblem {
	var out []uploadProblem
	host := func(msg string) { out = append(out, uploadProblem{fieldHosts, msg}) }
	if len(hosts) > models.MaxPluginHosts {
		host(fmt.Sprintf("The plugin asks to reach more than %d hosts.", models.MaxPluginHosts))
		return out
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		switch {
		case models.CheckPluginHost(h.Address) != nil:
			host(fmt.Sprintf("The plugin asks to reach “%s”, which is not a host and port such as nets.example.org:443.", h.Address))
		case seen[h.Address]:
			host(fmt.Sprintf("The plugin asks to reach “%s” twice.", h.Address))
		case len(h.Reason) > models.MaxPluginHostReason:
			host(fmt.Sprintf("The plugin's reason to reach “%s” must be at most %d characters.", h.Address, models.MaxPluginHostReason))
		}
		seen[h.Address] = true
	}
	return out
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
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuse(c, http.StatusRequestEntityTooLarge, []uploadProblem{tooLarge(c.Request.ContentLength)}, nil)
			return nil, false
		}
		refuse(c, http.StatusBadRequest, []uploadProblem{{fieldFile, msgNotAPlugin}}, nil)
		return nil, false
	}
	if fh.Size > maxUpload {
		refuse(c, http.StatusRequestEntityTooLarge, []uploadProblem{tooLarge(fh.Size)}, nil)
		return nil, false
	}
	f, err := fh.Open()
	if err != nil {
		refuse(c, http.StatusBadRequest, []uploadProblem{{fieldFile, msgNotAPlugin}}, nil)
		return nil, false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxUpload))
	if err != nil {
		refuse(c, http.StatusBadRequest, []uploadProblem{{fieldFile, msgNotAPlugin}}, nil)
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

func manifestOf(info models.PluginInfo, ranges []models.PluginRange) apimodels.PluginManifest {
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
	return out
}

// inspect runs module until it says hello, and answers each problem when
// it is not a plugin or says what a plugin may not. The IDs it would get
// are ranges, or the IDs it asks for when ranges is nil. A new plugin is
// checked also for its name and its IDs.
func inspect(c *gin.Context, db *gorm.DB, module []byte, ranges []models.PluginRange, newPlugin bool) (models.PluginInfo, []models.PluginRange, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), inspectTime)
	defer cancel()
	engine := engineOf(c)
	hello, err := engine.Inspect(ctx, module)
	if err != nil {
		slog.Info("Plugins: an upload is not a plugin", "error", err)
		refuse(c, http.StatusBadRequest, []uploadProblem{startProblem(err, engine.Limits())}, nil)
		return models.PluginInfo{}, nil, false
	}
	info, asked := infoOf(hello)
	if ranges == nil {
		ranges = asked
	}
	if problems := helloProblems(db, info, ranges, newPlugin); len(problems) > 0 {
		slog.Info("Plugins: an upload says what a plugin may not", "problems", len(problems))
		manifest := manifestOf(info, asked)
		refuse(c, http.StatusBadRequest, problems, &manifest)
		return models.PluginInfo{}, nil, false
	}
	return info, asked, true
}

// POSTPluginInspect answers what an uploaded plugin file says it is and
// asks for, and adds nothing.
func POSTPluginInspect(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	module, ok := readModule(c)
	if !ok {
		return
	}
	info, ranges, ok := inspect(c, db, module, nil, true)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, manifestOf(info, ranges))
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
		refuse(c, http.StatusBadRequest, []uploadProblem{{fieldIDs, msgBadForm}}, nil)
		return
	}
	if a := c.PostForm("allowed_hosts"); a != "" {
		if err := json.Unmarshal([]byte(a), &allowed); err != nil {
			refuse(c, http.StatusBadRequest, []uploadProblem{{fieldHosts, msgBadForm}}, nil)
			return
		}
	}
	info, _, ok := inspect(c, db, module, rangesOf(ranges), true)
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
	info, _, ok := inspect(c, db, module, nil, false)
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
