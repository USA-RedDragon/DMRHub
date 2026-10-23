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

package utils

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetSessionUser returns the logged-in user. It answers the request and
// returns false when there is none.
func GetSessionUser(c *gin.Context, db *gorm.DB) (models.User, bool) {
	uid, ok := sessions.Default(c).Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
		return models.User{}, false
	}
	var user models.User
	err := db.First(&user, uid).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
		return models.User{}, false
	case err != nil:
		slog.Error("Error finding the session user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return models.User{}, false
	}
	return user, true
}
