// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func POSTLogin(c *gin.Context) {
	session := sessions.Default(c)
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		klog.Errorf("POSTLogin: Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	var json apimodels.AuthLogin
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTLogin: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		// Check that one of username or callsign is not blank
		if json.Username == "" && json.Callsign == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username or Callsign must be provided"})
			return
		}
		// Check that password isn't a zero string
		if json.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password cannot be blank"})
			return
		}
		var user models.User
		if json.Username != "" {
			db.Find(&user, "username = ?", json.Username)
		} else {
			db.Find(&user, "callsign = ?", json.Callsign)
		}
		if user.ID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		verified, err := utils.VerifyPassword(json.Password, user.Password, config.GetConfig().PasswordSalt)
		klog.Infof("POSTLogin: Password verified %v", verified)
		if verified && err == nil {
			if user.Suspended {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "User is suspended"})
				return
			}
			if user.Approved {
				session.Set("user_id", user.ID)
				err = session.Save()
				if err != nil {
					klog.Errorf("POSTLogin: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving session"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "Logged in"})
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User is not approved"})
			return
		}
		klog.Errorf("POSTLogin: %v", err)
	}

	c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
}

func GETLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		klog.Errorf("GETLogout: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}
