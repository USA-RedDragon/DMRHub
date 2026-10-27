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
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/ratelimit"
	"github.com/USA-RedDragon/DMRHub/internal/http/sessionstore"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// MailerKey is the key of the smtp.Mailer in the gin context.
const MailerKey = "Mailer"

const (
	msgResetSent    = "If that account has an email address, a link is on its way. It works for 1 hour."
	msgResetDone    = "Your password is set. Log in with it now."
	msgResetBadLink = "This link does not work. It is older than 1 hour or was used already. Ask for a new one."
	msgResetNoUser  = "Enter your username or callsign."
	msgResetNoPass  = "Enter a new password."

	resetsPerAccount = 3
	resetsPerIP      = 10
	resetLimitWindow = time.Hour
	ipv6HostBits     = 64
)

// ResetLimits caps the password reset emails per account and per client
// address.
type ResetLimits struct {
	account *ratelimit.GORMStore
	ip      *ratelimit.GORMStore
}

// NewResetLimits returns the limits of 3 password reset emails per account
// and 10 requests per client IPv4 address or IPv6 /64, each per hour.
func NewResetLimits(db *gorm.DB) *ResetLimits {
	return &ResetLimits{
		account: ratelimit.NewGORMStore(&ratelimit.GORMOptions{DB: db, Rate: resetLimitWindow, Limit: resetsPerAccount}),
		ip:      ratelimit.NewGORMStore(&ratelimit.GORMOptions{DB: db, Rate: resetLimitWindow, Limit: resetsPerIP}),
	}
}

type forgotPassword struct {
	User string `json:"user"`
}

type passwordReset struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// POSTForgotPassword emails a link to choose a new password to the account
// with the username or callsign of the request. It gives the same answer
// whether or not the account exists, has an email address, or is over its
// limit, so that nobody can learn which accounts exist.
func POSTForgotPassword(limits *ResetLimits) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := utils.GetConfig(c)
		if !ok {
			return
		}
		if !config.SMTP.Enabled {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		db, ok := utils.GetDB(c)
		if !ok {
			return
		}
		var json forgotPassword
		if err := c.ShouldBindJSON(&json); err != nil || strings.TrimSpace(json.User) == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetNoUser})
			return
		}
		now := time.Now()
		if err := models.DeleteExpiredPasswordResets(db, now); err != nil {
			slog.Error("Could not delete expired password resets", "error", err)
		}
		sendReset(c, db, limits, strings.TrimSpace(json.User), now)
		c.JSON(http.StatusOK, gin.H{messageKey: msgResetSent})
	}
}

func sendReset(c *gin.Context, db *gorm.DB, limits *ResetLimits, name string, now time.Time) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	if limits.ip.Limit("password-reset:ip:"+clientNetwork(c.ClientIP()), c).RateLimited {
		return
	}
	user, found, err := findResetUser(db, name)
	if err != nil {
		slog.Error("Could not find the user of a password reset", "error", err)
		return
	}
	if !found || user.Email == "" || !user.EmailVerified {
		return
	}
	if limits.account.Limit("password-reset:user:"+strconv.FormatUint(uint64(user.ID), 10), c).RateLimited {
		return
	}
	mailer, ok := c.Value(MailerKey).(smtp.Mailer)
	if !ok {
		slog.Error("Mailer not found in context, no password reset email is sent", "user", user.ID)
		return
	}
	token, err := models.CreatePasswordReset(db, user.ID, now)
	if err != nil {
		slog.Error("Could not make a password reset", "user", user.ID, "error", err)
		return
	}
	subject := "Choose a new " + config.NetworkName + " password"
	body := fmt.Sprintf("Someone asked for a new password for %s on %s.\n\n"+
		"To choose one, open this link. It works once, for 1 hour:\n%s/reset-password#token=%s\n\n"+
		"If you did not ask, ignore this email. Your password stays the same.\n",
		user.Callsign, config.NetworkName, strings.TrimRight(config.HTTP.CanonicalHost, "/"), token)
	go func(id uint, to string) {
		if err := mailer.SendText(to, subject, body); err != nil {
			slog.Error("Could not send a password reset email", "user", id, "error", err)
		}
	}(user.ID, user.Email)
}

// clientNetwork returns the IPv4 address ip, or the /64 network of the
// IPv6 address ip, since one host often holds a whole IPv6 /64.
func clientNetwork(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.WithZone("").Prefix(ipv6HostBits)
	if err != nil {
		return ip
	}
	return prefix.String()
}

func findResetUser(db *gorm.DB, name string) (models.User, bool, error) {
	user, ok, err := models.FindUserByUsername(db, name)
	if err == nil && !ok {
		user, ok, err = models.FindUserByCallsign(db, name)
	}
	if err != nil {
		return models.User{}, false, fmt.Errorf("could not find the account to reset: %w", err)
	}
	return user, ok, nil
}

// POSTResetPasswordCheck answers the callsign of the account of a working
// password reset link, so that its page can name the account.
func POSTResetPasswordCheck(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var json passwordReset
	if err := c.ShouldBindJSON(&json); err != nil || json.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetBadLink})
		return
	}
	user, found, err := models.PasswordResetUser(db, json.Token, time.Now())
	if err != nil {
		slog.Error("Could not check a password reset", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetBadLink})
		return
	}
	c.JSON(http.StatusOK, gin.H{"callsign": user.Callsign})
}

// POSTResetPassword sets the password of the account of a password reset
// link, uses up the link, and ends every session and app grant of the
// account.
func POSTResetPassword(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	var json passwordReset
	if err := c.ShouldBindJSON(&json); err != nil || json.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetBadLink})
		return
	}
	if json.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetNoPass})
		return
	}
	now := time.Now()
	if _, found, err := models.PasswordResetUser(db, json.Token, now); err != nil || !found {
		if err != nil {
			slog.Error("Could not check a password reset", "error", err)
		}
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetBadLink})
		return
	}
	if status, msg := utils.CheckPwnedPassword(c, config.HIBPAPIKey, json.Password); status != 0 {
		c.JSON(status, gin.H{errorKey: msg})
		return
	}
	hash, err := utils.HashPassword(json.Password, config.PasswordSalt)
	if err != nil {
		slog.Error("Error hashing password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error hashing password"})
		return
	}
	id, done, err := models.ResetPassword(db, json.Token, hash, now)
	if err != nil {
		slog.Error("Could not reset a password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryAgainLater})
		return
	}
	if !done {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgResetBadLink})
		return
	}
	if !endEverySession(c, id) || !utils.RevokeUserGrants(c, id) {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Password set, but the sessions could not be ended"})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: msgResetDone})
}

func endEverySession(c *gin.Context, userID uint) bool {
	store, ok := c.Value("SessionStore").(*sessionstore.Store)
	if !ok {
		slog.Error("SessionStore not found in context", "user", userID)
		return false
	}
	if err := store.DeleteUserSessions(c.Request.Context(), userID, ""); err != nil {
		slog.Error("Failed to end the sessions of a user", "user", userID, "error", err)
		return false
	}
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a user stay open", "user", userID)
		return false
	}
	if err := websocket.PublishPasswordChanged(ps, userID, ""); err != nil {
		slog.Error("Failed to close the websockets of a user", "user", userID, "error", err)
		return false
	}
	return true
}
