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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	errorKey        = "error"
	messageKey      = "message"
	msgLoginBlocked = "Too many failed logins. Try again in 15 minutes."
)

// loginAccount names the account of a login for the login backoff: the
// user by ID, or the name of the login in lower case when no user has it.
func loginAccount(user models.User, found bool, json apimodels.AuthLogin) string {
	if found {
		return "user:" + strconv.FormatUint(uint64(user.ID), 10)
	}
	name := json.Username
	if name == "" {
		name = json.Callsign
	}
	name = strings.ToLower(name)
	if len(name) > 70 {
		name = name[:70]
	}
	return "name:" + name
}

// loginMinDuration is the minimum time the login handler will take once
// calibrated. It is set on the first login attempt by running a full
// argon2 derivation and adding 50% headroom.
var (
	loginMinDuration time.Duration //nolint:gochecknoglobals
	loginMinOnce     sync.Once     //nolint:gochecknoglobals
)

// calibrateLoginTiming runs a single dummy password verification to
// measure the hardware-specific argon2 cost, then sets the floor at
// 150% of that duration. All subsequent login attempts will sleep out
// any remaining time so that the total latency is constant regardless
// of whether the user account exists.
func calibrateLoginTiming() {
	start := time.Now()
	//nolint:errcheck,gosec // result irrelevant, we only care about duration
	utils.VerifyPassword("calibration", utils.DummyHash(), "")
	elapsed := time.Since(start)
	loginMinDuration = elapsed + elapsed/2
}

func POSTLogin(c *gin.Context) {
	session := sessions.Default(c)
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	var json apimodels.AuthLogin
	err := c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTLogin", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "JSON data is invalid"})
	} else {
		// Check that one of username or callsign is not blank
		if json.Username == "" && json.Callsign == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username or Callsign must be provided"})
			return
		}
		// Check that password isn't a zero string
		if json.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Password cannot be blank"})
			return
		}
		var user models.User
		var userFound bool

		// Calibrate the login timing floor on the first login attempt.
		// This runs a full argon2 derivation to measure hardware-specific
		// cost and sets the minimum response time at 150% of that.
		loginMinOnce.Do(calibrateLoginTiming)

		// Start timing after input validation to normalize execution time.
		// This prevents attackers from distinguishing existing vs non-existing
		// users by measuring response latency (timing oracle attack).
		loginStart := time.Now()

		var findErr error
		if json.Username != "" {
			user, userFound, findErr = models.FindUserByUsername(db, json.Username)
			if findErr == nil && !userFound {
				user, userFound, findErr = models.FindUserByCallsign(db, json.Username)
			}
		} else {
			user, userFound, findErr = models.FindUserByCallsign(db, json.Callsign)
		}
		if findErr != nil {
			slog.Error("Failed to find the user", "function", "POSTLogin", "error", findErr)
		}
		account := loginAccount(user, userFound, json)
		if config.LoginBackoff {
			blocked, err := models.LoginBlocked(db, account, time.Now())
			if err != nil {
				slog.Error("Failed to count the failed logins", "function", "POSTLogin", "error", err)
			}
			if blocked {
				if remaining := loginMinDuration - time.Since(loginStart); remaining > 0 {
					time.Sleep(remaining)
				}
				c.JSON(http.StatusTooManyRequests, gin.H{errorKey: msgLoginBlocked})
				return
			}
		}

		// When the user is not found, compare against a dummy hash to prevent
		// timing oracle attacks. DummyHash() is generated by HashPassword with
		// the same parameters and byte lengths as real password hashes.
		compareHash := user.Password
		if !userFound {
			compareHash = utils.DummyHash()
		}

		verified, err := utils.VerifyPassword(json.Password, compareHash, config.PasswordSalt)

		// Sleep out any remaining time so that every login attempt takes
		// at least loginMinDuration, hiding DB-level timing differences.
		if remaining := loginMinDuration - time.Since(loginStart); remaining > 0 {
			time.Sleep(remaining)
		}
		slog.Debug("Password verification completed", "function", "POSTLogin", "verified", verified, "userID", user.ID)
		if userFound && verified && err == nil {
			if config.LoginBackoff {
				if err := models.ClearLoginFailures(db, account); err != nil {
					slog.Error("Failed to clear the failed logins", "function", "POSTLogin", "error", err)
				}
			}
			if user.Suspended {
				c.JSON(http.StatusUnauthorized, gin.H{errorKey: "User is suspended"})
				return
			}
			if user.Approved {
				session.Set("user_id", user.ID)
				err = session.Save()
				if err != nil {
					slog.Error("Error saving session", "function", "POSTLogin", "error", err, "userID", user.ID)
					c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error saving session"})
					return
				}
				c.JSON(http.StatusOK, gin.H{messageKey: "Logged in"})
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{errorKey: "User is not approved"})
			return
		}
		slog.Error("Password verification failed", "function", "POSTLogin", "error", err)
		if config.LoginBackoff {
			if err := models.AddLoginFailure(db, account, time.Now()); err != nil {
				slog.Error("Failed to store a failed login", "function", "POSTLogin", "error", err)
			}
		}
	}

	c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
}

func POSTLogout(c *gin.Context) {
	session := sessions.Default(c)
	userID, loggedIn := session.Get("user_id").(uint)
	sessionID := session.ID()
	session.Clear()
	err := session.Save()
	if err != nil {
		slog.Error("Error saving session during logout", "function", "POSTLogout", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error saving session"})
		return
	}
	if loggedIn {
		if ps, ok := c.Value("PubSub").(pubsub.PubSub); ok {
			if err := websocket.PublishLogout(ps, userID, sessionID); err != nil {
				slog.Error("Failed to close the websockets of a logged out session", "function", "POSTLogout", "error", err)
			}
		} else {
			slog.Error("PubSub not found in context, the websockets of a logged out session stay open", "function", "POSTLogout")
		}
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "Logged out"})
}
