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

package auth

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	// MsgEmailFormat answers an email address that is not valid.
	MsgEmailFormat = "Enter an email address, such as n0call@example.com"
	// MsgNoEmail answers an email address on a network that cannot send email.
	MsgNoEmail = "This network cannot send email"

	msgTryAgainLater = "Try again later"
	msgEmailSet      = "Email saved. Open the link we sent to it to confirm it."
	msgEmailRemoved  = "Email removed."
	msgEmailVerified = "Your email address is confirmed."
	msgEmailBadLink  = "This link does not work. It is older than 24 hours, was used already, or is for an older address."
)

type emailChange struct {
	Email string `json:"email"`
}

type emailVerification struct {
	Token string `json:"token"`
}

// CheckEmail answers the request and returns false when email cannot be
// the email address of an account: it is not a valid address, or the
// network cannot send email. An empty email removes the address.
func CheckEmail(c *gin.Context, email string) bool {
	config, ok := utils.GetConfig(c)
	if !ok {
		return false
	}
	if email == "" {
		return true
	}
	if !config.SMTP.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: MsgNoEmail})
		return false
	}
	if !utils.ValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: MsgEmailFormat})
		return false
	}
	return true
}

// SendEmailVerification emails the link that verifies the address email
// of the user id. With limits, it sends at most the reset limit of links
// per account each hour.
func SendEmailVerification(c *gin.Context, db *gorm.DB, limits *ResetLimits, id uint, callsign, email string) {
	config, ok := utils.GetConfig(c)
	if !ok || !config.SMTP.Enabled || email == "" {
		return
	}
	if limits != nil && limits.account.Limit("email-verification:user:"+strconv.FormatUint(uint64(id), 10), c).RateLimited {
		slog.Info("Not sending an email verification, the account sent too many", "user", id)
		return
	}
	mailer, ok := c.Value(MailerKey).(smtp.Mailer)
	if !ok {
		slog.Error("Mailer not found in context, no email verification is sent", "user", id)
		return
	}
	now := time.Now()
	if err := models.DeleteExpiredEmailVerifications(db, now); err != nil {
		slog.Error("Could not delete expired email verifications", "error", err)
	}
	token, err := models.CreateEmailVerification(db, id, email, now)
	if err != nil {
		slog.Error("Could not make an email verification", "user", id, "error", err)
		return
	}
	subject := "Confirm your " + config.NetworkName + " email address"
	body := fmt.Sprintf("Someone gave this email address for %s on %s.\n\n"+
		"To confirm it, open this link. It works once, for 24 hours:\n%s/verify-email#token=%s\n\n"+
		"If it was not you, ignore this email.\n",
		callsign, config.NetworkName, strings.TrimRight(config.HTTP.CanonicalHost, "/"), token)
	go func() {
		if err := mailer.SendText(email, subject, body); err != nil {
			slog.Error("Could not send an email verification", "user", id, "error", err)
		}
	}()
}

// PUTMyEmail sets the email address of the logged-in user, which stays
// unverified until its link is used. An empty address removes it.
func PUTMyEmail(limits *ResetLimits) gin.HandlerFunc {
	return func(c *gin.Context) {
		db, ok := utils.GetDB(c)
		if !ok {
			return
		}
		user, ok := utils.GetSessionUser(c, db)
		if !ok {
			return
		}
		var json emailChange
		if err := c.ShouldBindJSON(&json); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: MsgEmailFormat})
			return
		}
		email := strings.TrimSpace(json.Email)
		if !CheckEmail(c, email) {
			return
		}
		if err := models.SetEmail(db, user.ID, email); err != nil {
			slog.Error("Could not set an email address", "user", user.ID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
			return
		}
		if email == "" {
			c.JSON(http.StatusOK, gin.H{messageKey: msgEmailRemoved})
			return
		}
		SendEmailVerification(c, db, limits, user.ID, user.Callsign, email)
		c.JSON(http.StatusOK, gin.H{messageKey: msgEmailSet})
	}
}

// POSTVerifyEmail uses the link that verifies an email address.
func POSTVerifyEmail(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var json emailVerification
	if err := c.ShouldBindJSON(&json); err != nil || json.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgEmailBadLink})
		return
	}
	verified, err := models.VerifyEmail(db, json.Token, time.Now())
	if err != nil {
		slog.Error("Could not verify an email address", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !verified {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgEmailBadLink})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: msgEmailVerified})
}
