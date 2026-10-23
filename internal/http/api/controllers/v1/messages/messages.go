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

// Package messages serves the inbox, outbox and stats of the logged-in user
// and the message search of the admins.
package messages

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey      = "error"
	msgTryLater   = "Try again later"
	msgNotFound   = "Message not found"
	defaultLimit  = 10
	maxLimit      = 50
	statsDays     = 7
	dayFormat     = "2006-01-02"
	responseACK   = string(models.CallResponseACK)
	responseNACK  = string(models.CallResponseNACK)
	queryParamTZ  = "tz"
	queryParamPg  = "page"
	queryParamLim = "limit"
)

func intParam(c *gin.Context, name string, def, maxValue int) int {
	v, err := strconv.Atoi(c.Query(name))
	if err != nil || v < 1 {
		return def
	}
	if maxValue > 0 && v > maxValue {
		return maxValue
	}
	return v
}

func failed(c *gin.Context, what string, err error) {
	slog.Error("Messages: "+what, "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
}

func box(c *gin.Context, which models.MessageBox) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	ids, err := models.VisibleMessageIDs(db, &user, which)
	if err != nil {
		failed(c, "cannot list a box", err)
		return
	}
	limit := intParam(c, queryParamLim, defaultLimit, maxLimit)
	page := intParam(c, queryParamPg, 1, 0)
	out := apimodels.MessageList{Total: len(ids), Messages: []apimodels.Message{}}
	start := (page - 1) * limit
	if page > models.MessageBoxCap || start >= len(ids) {
		c.JSON(http.StatusOK, out)
		return
	}
	pageIDs := ids[start:min(start+limit, len(ids))]
	msgs, err := load(db, pageIDs)
	if err != nil {
		failed(c, "cannot load a box", err)
		return
	}
	if out.Messages, err = messages.UserView(db, msgs); err != nil {
		failed(c, "cannot show a box", err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func load(db *gorm.DB, ids []uint) ([]models.Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var msgs []models.Message
	if err := db.Where("id IN ?", ids).Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("could not load messages: %w", err)
	}
	byID := make(map[uint]models.Message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	out := make([]models.Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// GETInbox returns a page of the private messages to the radio IDs of the
// logged-in user.
func GETInbox(c *gin.Context) {
	box(c, models.MessageInbox)
}

// GETOutbox returns a page of the messages from the radio IDs of the
// logged-in user.
func GETOutbox(c *gin.Context) {
	box(c, models.MessageOutbox)
}

// GETMessage returns one message of the inbox or outbox of the logged-in
// user.
func GETMessage(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	boxes, err := models.MessageBoxes(db, &user, uint(id))
	if err != nil {
		failed(c, "cannot find the boxes of a message", err)
		return
	}
	if len(boxes) == 0 {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	msgs, err := load(db, []uint{uint(id)})
	if err != nil || len(msgs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	view, err := messages.UserView(db, msgs)
	if err != nil {
		failed(c, "cannot show a message", err)
		return
	}
	c.JSON(http.StatusOK, view[0])
}

// GETStats counts the messages of the inbox and outbox of the logged-in
// user, over the same capped boxes that GETInbox and GETOutbox show.
func GETStats(c *gin.Context) {
	loc := time.UTC
	if tz := c.Query(queryParamTZ); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Unknown time zone"})
			return
		}
		loc = l
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	inbox, err := models.VisibleMessageIDs(db, &user, models.MessageInbox)
	if err != nil {
		failed(c, "cannot list the inbox", err)
		return
	}
	outbox, err := models.VisibleMessageIDs(db, &user, models.MessageOutbox)
	if err != nil {
		failed(c, "cannot list the outbox", err)
		return
	}
	seen := make(map[uint]bool, len(inbox)+len(outbox))
	all := make([]uint, 0, len(inbox)+len(outbox))
	for _, id := range append(append([]uint{}, inbox...), outbox...) {
		if !seen[id] {
			seen[id] = true
			all = append(all, id)
		}
	}
	var rows []models.Message
	if len(all) > 0 {
		if err := db.Select("id", "received_at", "format", "response").Where("id IN ?", all).Find(&rows).Error; err != nil {
			failed(c, "cannot load the stats", err)
			return
		}
	}
	stats := apimodels.MessageStats{Inbox: len(inbox), Outbox: len(outbox), ByFormat: map[string]int{}}
	today := time.Now().In(loc)
	days := make(map[string]int, statsDays)
	for i := statsDays - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format(dayFormat)
		days[d] = len(stats.ByDay)
		stats.ByDay = append(stats.ByDay, apimodels.MessageDay{Day: d})
	}
	for _, m := range rows {
		stats.ByFormat[string(m.Format)]++
		switch string(m.Response) {
		case responseACK:
			stats.ACK++
		case responseNACK:
			stats.NACK++
		}
		if i, ok := days[m.ReceivedAt.In(loc).Format(dayFormat)]; ok {
			stats.ByDay[i].Count++
		}
	}
	c.JSON(http.StatusOK, stats)
}
