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

package messages

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	adminDefaultLimit = 25
	adminMaxLimit     = 100
	maxAccessPage     = 1000000
)

var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_") //nolint:gochecknoglobals

func validFormat(f string) bool {
	switch models.MessageFormat(f) {
	case models.MessageFormatTMS, models.MessageFormatIP, models.MessageFormatAnyTone, models.MessageFormatDefinedData,
		models.MessageFormatHytera, models.MessageFormatData, models.MessageFormatUnknown:
		return true
	}
	return false
}

func search(c *gin.Context, db *gorm.DB) (*gorm.DB, string) {
	q := db.Model(&models.Message{})
	for _, p := range []struct{ name, column string }{{"src", "source_id"}, {"dst", "destination_id"}} {
		v := c.Query(p.name)
		if v == "" {
			continue
		}
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, "Invalid " + p.name
		}
		q = q.Where(p.column+" = ?", uint(id))
	}
	if f := c.Query("format"); f != "" {
		if !validFormat(f) {
			return nil, "Invalid format"
		}
		q = q.Where("format = ?", f)
	}
	for _, p := range []struct{ name, op string }{{"from", ">="}, {"to", "<"}} {
		v := c.Query(p.name)
		if v == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, "Invalid " + p.name + " time"
		}
		q = q.Where("received_at "+p.op+" ?", at.UTC())
	}
	if text := c.Query("text"); text != "" {
		q = q.Where("LOWER(text) LIKE ? ESCAPE '!'", "%"+likeEscaper.Replace(strings.ToLower(text))+"%")
	}
	return q, ""
}

// logAccess logs that the admin of the request saw msgs. When it cannot,
// it answers the request and returns false, so that no admin sees a
// message that the log does not show.
func logAccess(c *gin.Context, db *gorm.DB, kind models.MessageAccessKind, id *uint, msgs []models.Message) bool {
	admin, _ := sessions.Default(c).Get("user_id").(uint)
	if err := models.LogMessageAccess(db, admin, kind, id, c.Request.URL.RawQuery, msgs, time.Now()); err != nil {
		failed(c, "cannot log a message access", err)
		return false
	}
	return true
}

// GETAdminMessageAccesses lists the times that admins saw stored messages,
// newest first, with the radio IDs whose messages they saw and the users
// of those radio IDs. The admin and radio_id parameters pick the accesses
// of one admin and of the messages of one radio ID.
func GETAdminMessageAccesses(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var q models.MessageAccessQuery
	for _, p := range []struct {
		name string
		into *uint
	}{{"admin", &q.AdminID}, {"radio_id", &q.RadioID}} {
		v := c.Query(p.name)
		if v == "" {
			continue
		}
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid " + p.name})
			return
		}
		*p.into = uint(id)
	}
	limit := intParam(c, queryParamLim, adminDefaultLimit, adminMaxLimit)
	page := intParam(c, queryParamPg, 1, maxAccessPage)
	rows, ids, total, err := models.ListMessageAccesses(db, q, (page-1)*limit, limit)
	if err != nil {
		failed(c, "cannot list the message accesses", err)
		return
	}
	out, err := apimodels.NewMessageAccessList(db, rows, ids, total)
	if err != nil {
		failed(c, "cannot show the message accesses", err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// GETAdminMessages searches every stored message, with no opt-in filter and
// no cap.
func GETAdminMessages(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	q, bad := search(c, db)
	if bad != "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: bad})
		return
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		failed(c, "cannot count a search", err)
		return
	}
	limit := intParam(c, queryParamLim, adminDefaultLimit, adminMaxLimit)
	page := intParam(c, queryParamPg, 1, 0)
	out := apimodels.AdminMessageList{Total: total, Messages: []apimodels.AdminMessage{}}
	pages := (total + int64(limit) - 1) / int64(limit)
	if int64(page) > pages {
		c.JSON(http.StatusOK, out)
		return
	}
	offset := int64(page-1) * int64(limit)
	var msgs []models.Message
	if err := q.Omit("raw").Order("received_at DESC").Order("id DESC").Offset(int(offset)).Limit(limit).Find(&msgs).Error; err != nil {
		failed(c, "cannot search", err)
		return
	}
	view, err := messages.AdminView(db, msgs, false)
	if err != nil {
		failed(c, "cannot show a search", err)
		return
	}
	if !logAccess(c, db, models.MessageAccessSearch, nil, msgs) {
		return
	}
	out.Messages = view
	c.JSON(http.StatusOK, out)
}

// GETAdminMessage returns one stored message with its raw octets.
func GETAdminMessage(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	var msg models.Message
	err = db.First(&msg, uint(id)).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	case err != nil:
		failed(c, "cannot load a message", err)
		return
	}
	view, err := messages.AdminView(db, []models.Message{msg}, true)
	if err != nil {
		failed(c, "cannot show a message", err)
		return
	}
	if !logAccess(c, db, models.MessageAccessRead, &msg.ID, []models.Message{msg}) {
		return
	}
	c.JSON(http.StatusOK, view[0])
}
