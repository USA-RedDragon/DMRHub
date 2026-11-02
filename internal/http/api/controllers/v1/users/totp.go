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
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"rsc.io/qr"
)

// OnAirUnlock is an unlock of a radio ID of the user on one slot of a
// repeater or hotspot, as the Settings page lists it.
type OnAirUnlock struct {
	Radio    uint      `json:"radio"`
	Repeater uint      `json:"repeater"`
	Callsign string    `json:"callsign"`
	Slot     uint8     `json:"slot"`
	Until    time.Time `json:"until"`
}

// OnAirCode is the state of the on-air code of the user.
type OnAirCode struct {
	On            bool          `json:"on"`
	UnlockSeconds int64         `json:"unlock_seconds"`
	Unlocks       []OnAirUnlock `json:"unlocks"`
}

// OnAirSetup is a new secret for an authenticator app. The API shows it
// once.
type OnAirSetup struct {
	Key string `json:"key"`
	URI string `json:"uri"`
	QR  string `json:"qr"`
}

// OnAirCodeBody holds a code from the authenticator app.
type OnAirCodeBody struct {
	Code string `json:"code"`
}

func onAirHub(c *gin.Context) (*hub.Hub, bool) {
	if utils.ViaToken(c) {
		c.JSON(http.StatusForbidden, gin.H{errorKey: "Sign in to change your on-air code"})
		return nil, false
	}
	h, _ := c.MustGet("Hub").(*hub.Hub)
	if h == nil || h.OnAirCodes() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{errorKey: msgTryLater})
		return nil, false
	}
	return h, true
}

func onAirUser(c *gin.Context) (*hub.Hub, *gorm.DB, models.User, bool) {
	h, ok := onAirHub(c)
	if !ok {
		return nil, nil, models.User{}, false
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return nil, nil, models.User{}, false
	}
	user, ok := utils.GetSessionUser(c, db)
	return h, db, user, ok
}

func unlockSeconds(c *gin.Context) int64 {
	d := airauth.DefaultUnlock
	if cfg, ok := utils.GetConfig(c); ok && cfg.DMR.Routing.OnAirUnlock > 0 {
		d = cfg.DMR.Routing.OnAirUnlock
	}
	return int64(d / time.Second)
}

func onAirState(c *gin.Context, h *hub.Hub, db *gorm.DB, userID uint) (OnAirCode, error) {
	svc := h.OnAirCodes()
	on, err := svc.Enabled(userID)
	if err != nil {
		return OnAirCode{}, err //nolint:wrapcheck // logged by the caller
	}
	out := OnAirCode{On: on, UnlockSeconds: unlockSeconds(c), Unlocks: []OnAirUnlock{}}
	if !on {
		return out, nil
	}
	grants, err := svc.UserGrants(c.Request.Context(), userID)
	if err != nil {
		return OnAirCode{}, err //nolint:wrapcheck // logged by the caller
	}
	ids := make([]uint, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.Repeater)
	}
	callsigns := map[uint]string{}
	if len(ids) > 0 {
		var rows []struct {
			ID       uint
			Callsign string
		}
		if err := db.Model(&models.Repeater{}).Where("id IN ?", ids).Select("id, callsign").Scan(&rows).Error; err != nil {
			return OnAirCode{}, err //nolint:wrapcheck // logged by the caller
		}
		for _, r := range rows {
			callsigns[r.ID] = r.Callsign
		}
	}
	for _, g := range grants {
		out.Unlocks = append(out.Unlocks, OnAirUnlock{Radio: g.Radio, Repeater: g.Repeater, Callsign: callsigns[g.Repeater], Slot: g.Slot, Until: g.Until})
	}
	slices.SortFunc(out.Unlocks, func(a, b OnAirUnlock) int { return a.Until.Compare(b.Until) })
	return out, nil
}

func replyOnAir(c *gin.Context, h *hub.Hub, db *gorm.DB, userID uint) {
	state, err := onAirState(c, h, db, userID)
	if err != nil {
		slog.Error("Failed to read the on-air code", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	c.JSON(http.StatusOK, state)
}

// GETMyOnAirCode returns whether the on-air code of the logged-in user is
// on, and the unlocks of their radio IDs.
func GETMyOnAirCode(c *gin.Context) {
	h, db, user, ok := onAirUser(c)
	if !ok {
		return
	}
	replyOnAir(c, h, db, user.ID)
}

// POSTMyOnAirSetup makes a new secret of the on-air code for the logged-in
// user and returns it once, with its QR code. The code stays off until
// POSTMyOnAirCode confirms a code of the secret.
func POSTMyOnAirSetup(c *gin.Context) {
	h, _, user, ok := onAirUser(c)
	if !ok {
		return
	}
	issuer := "DMRHub"
	if cfg, ok := utils.GetConfig(c); ok && cfg.NetworkName != "" {
		issuer = cfg.NetworkName
	}
	setup, err := h.OnAirCodes().Begin(c.Request.Context(), user.ID, issuer, user.Callsign)
	if errors.Is(err, airauth.ErrOn) {
		c.JSON(http.StatusConflict, gin.H{errorKey: "Your on-air code is already on"})
		return
	}
	if err != nil {
		slog.Error("Failed to set up an on-air code", "userID", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	img, err := qrDataURL(setup.URI)
	if err != nil {
		slog.Error("Failed to draw the QR code of an on-air code", "userID", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, OnAirSetup{Key: setup.Key, URI: setup.URI, QR: img})
}

func qrDataURL(uri string) (string, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return "", err //nolint:wrapcheck // logged by the caller
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}

func bindCode(c *gin.Context) (uint32, bool) {
	var body OnAirCodeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Enter the 6-digit code from your app"})
		return 0, false
	}
	code, ok := airauth.ParseCode(body.Code)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Enter the 6-digit code from your app"})
		return 0, false
	}
	return code, true
}

func codeError(c *gin.Context, userID uint, err error) {
	switch {
	case errors.Is(err, airauth.ErrWrongCode):
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Wrong code. Enter the code that your app shows now"})
	case errors.Is(err, airauth.ErrNoSetup):
		c.JSON(http.StatusConflict, gin.H{errorKey: "The setup ended. Start it again"})
	case errors.Is(err, airauth.ErrOff):
		c.JSON(http.StatusConflict, gin.H{errorKey: "Your on-air code is off"})
	case errors.Is(err, airauth.ErrLimited):
		c.JSON(http.StatusTooManyRequests, gin.H{errorKey: "Too many tries. Wait 5 minutes"})
	default:
		slog.Error("Failed to change the on-air code", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
	}
}

// POSTMyOnAirCode turns the on-air code of the logged-in user on with a
// code of the secret of the last setup.
func POSTMyOnAirCode(c *gin.Context) {
	h, db, user, ok := onAirUser(c)
	if !ok {
		return
	}
	code, ok := bindCode(c)
	if !ok {
		return
	}
	if err := h.OnAirCodes().Confirm(c.Request.Context(), user.ID, code); err != nil {
		codeError(c, user.ID, err)
		return
	}
	h.UserChanged(user.ID)
	replyOnAir(c, h, db, user.ID)
}

// DELETEMyOnAirCode turns the on-air code of the logged-in user off with a
// code from the app, and ends the unlocks of their radio IDs.
func DELETEMyOnAirCode(c *gin.Context) {
	h, db, user, ok := onAirUser(c)
	if !ok {
		return
	}
	code, ok := bindCode(c)
	if !ok {
		return
	}
	radios, err := h.OnAirCodes().Disable(c.Request.Context(), user.ID, code)
	if err != nil {
		codeError(c, user.ID, err)
		return
	}
	announceOff(h, user.ID, radios)
	replyOnAir(c, h, db, user.ID)
}

func announceOff(h *hub.Hub, userID uint, radios []uint) {
	h.UserChanged(userID)
	for _, r := range radios {
		h.UnlocksChanged(r)
	}
}

// DELETEMyOnAirUnlock locks a radio ID of the logged-in user again on one
// slot of a repeater or hotspot.
func DELETEMyOnAirUnlock(c *gin.Context) {
	h, db, user, ok := onAirUser(c)
	if !ok {
		return
	}
	radio, err1 := strconv.ParseUint(c.Param("radio"), 10, 32)
	repeater, err2 := strconv.ParseUint(c.Param("repeater"), 10, 32)
	slot, err3 := strconv.ParseUint(c.Param("slot"), 10, 8)
	if err1 != nil || err2 != nil || err3 != nil || (slot != 1 && slot != 2) {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Unknown unlock"})
		return
	}
	err := h.OnAirCodes().Lock(c.Request.Context(), user.ID, uint(radio), uint(repeater), uint8(slot))
	if errors.Is(err, airauth.ErrNotYours) {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Unknown unlock"})
		return
	}
	if err != nil {
		slog.Error("Failed to lock a radio ID", "userID", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	h.UnlocksChanged(uint(radio))
	replyOnAir(c, h, db, user.ID)
}

// DELETEUserOnAirCode turns the on-air code of a user off without a code,
// for an admin helping a user who lost their app.
func DELETEUserOnAirCode(c *gin.Context) {
	h, ok := onAirHub(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid user ID"})
		return
	}
	radios, err := h.OnAirCodes().Reset(c.Request.Context(), uint(id))
	if err != nil {
		slog.Error("Failed to turn off an on-air code", "userID", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
		return
	}
	announceOff(h, uint(id), radios)
	c.JSON(http.StatusOK, gin.H{"message": "The on-air code is off"})
}
