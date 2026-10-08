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
	"crypto/sha1" //#nosec G505 -- False positive, we are not using this for crypto, just HIBP
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gopwned "github.com/mavjs/goPwned"
)

const (
	usersKey          = "users"
	errorKey          = "error"
	totalKey          = "total"
	msgErrGettingUser = "Error getting user"
	messageKey        = "message"
	msgInvalidUserID  = "Invalid User ID"
	msgNotLoggedIn    = "Not logged in"
	msgErrSavingUser  = "Error saving user"
	msgUserNotFound   = "User does not exist"
	msgErrFindingUser = "Error finding user"
)

func GETUsers(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	users, err := models.ListUsers(db)
	if err != nil {
		slog.Error("Error getting users", "function", "GETUsers", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting users"})
		return
	}

	total, err := models.CountUsers(cDb)
	if err != nil {
		slog.Error("Error getting user count", "function", "GETUsers", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting user count"})
		return
	}
	c.JSON(http.StatusOK, gin.H{totalKey: total, usersKey: users})
}

// POSTUser is used to register a new user.
func POSTUser(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	var json apimodels.UserRegistration
	err := c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTUser", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "JSON data is invalid"})
	} else {
		if !config.DMR.DisableRadioIDValidation {
			if !userdb.IsValidUserID(json.DMRId) {
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "DMR ID is not valid"})
				return
			}
			if !userdb.ValidUserCallsign(json.DMRId, json.Callsign) {
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Callsign does not match DMR ID"})
				return
			}
		}

		isValid, errString := json.IsValidUsername()
		if !isValid {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: errString})
			return
		}

		// Check that password isn't a zero string
		if json.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Password cannot be blank"})
			return
		}

		// Check if the username is already taken
		var user models.User
		err := db.Find(&user, "username = ?", json.Username).Error
		if err != nil {
			slog.Error(msgErrGettingUser, "function", "POSTUser", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
			return
		} else if user.ID != 0 {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username is already taken"})
			return
		}

		// Check if the DMR ID is already taken
		exists, err := models.UserIDExists(db, json.DMRId)
		if err != nil {
			slog.Error(msgErrGettingUser, "function", "POSTUser", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
			return
		}
		if exists {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "DMR ID is already registered"})
			return
		}

		if config.HIBPAPIKey != "" {
			goPwned := gopwned.NewClient(nil, config.HIBPAPIKey)
			h := sha1.New() //#nosec G401 -- False positive, we are not using this for crypto, just HIBP
			h.Write([]byte(json.Password))
			sha1HashedPW := fmt.Sprintf("%X", h.Sum(nil))
			frange := sha1HashedPW[0:5]
			lrange := sha1HashedPW[5:40]
			karray, err := goPwned.GetPwnedPasswords(frange, false)
			if err != nil {
				// If the error message starts with "Too many requests", then tell the user to retry in one minute
				if strings.HasPrefix(err.Error(), "Too many requests") {
					c.JSON(http.StatusTooManyRequests, gin.H{errorKey: "Too many requests. Please try again in one minute"})
					return
				}
				slog.Error("Error getting pwned passwords", "function", "POSTUser", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting pwned passwords"})
				return
			}
			strKArray := string(karray)
			respArray := strings.Split(strKArray, "\r\n")

			var result int64
			for _, resp := range respArray {
				strArray := strings.Split(resp, ":")
				test := strArray[0]

				count, err := strconv.ParseInt(strArray[1], 0, 32)
				if err != nil {
					slog.Error("Error parsing pwned password count", "function", "POSTUser", "error", err)
					c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error parsing pwned password count"})
					return
				}
				if test == lrange {
					result = count
				}
			}
			if result > 0 {
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Password has been reported in a data breach. Please use another one"})
				return
			}
		}

		// argon2 the password
		hashedPassword, err := utils.HashPassword(json.Password, config.PasswordSalt)
		if err != nil {
			slog.Error("Error hashing password", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error hashing password"})
			return
		}

		// store the user in the database with Active = false
		user = models.User{
			Username: json.Username,
			Password: hashedPassword,
			Callsign: strings.ToUpper(json.Callsign),
			ID:       json.DMRId,
			Approved: false,
			Admin:    false,
		}
		err = db.Create(&user).Error
		if err != nil {
			slog.Error("Error creating user", "function", "POSTUser", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error creating user"})
			return
		}
		c.JSON(http.StatusOK, gin.H{messageKey: "User created, please wait for admin approval"})
		if config.SMTP.Enabled {
			err = smtp.SendToAdmins(
				config,
				db,
				"New user registration",
				fmt.Sprintf("A new user has registered.<br><br>Username: %s<br>Callsign: %s<br>DMR ID: %d<br><br><a href=\"%s/admin/users/approval\">Click here</a> to see the approval dashboard", json.Username, strings.ToUpper(json.Callsign), json.DMRId, config.HTTP.CanonicalHost),
			)
			if err != nil {
				slog.Error("Error sending email", "function", "POSTUser", "error", err)
			}
		}
		c.Set("new_user_id", user.ID)
	}
}

func POSTUserDemote(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	id := c.Param("id")

	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	session := sessions.Default(c)
	fromUserID, ok := session.Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgNotLoggedIn})
		return
	}
	if uint(userID) == fromUserID {
		// don't allow a user to demote themselves
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot demote yourself"})
		return
	}
	// Grab the user from the database
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUserDemote", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return
	}

	user.Admin = false
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "function", "POSTUserDemote", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User demoted"})

	if config.SMTP.Enabled {
		err = smtp.SendToAdmins(
			config,
			db,
			"Admin user demotion",
			fmt.Sprintf("An admin has been demoted.<br><br>Username: %s<br>Callsign: %s<br>DMR ID: %d", user.Username, strings.ToUpper(user.Callsign), user.ID),
		)
		if err != nil {
			slog.Error("Error sending email", "function", "POSTUserDemote", "error", err)
		}
	}
}

func POSTUserPromote(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}

	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	id := c.Param("id")
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	if idInt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid User ID: negative value"})
		return
	}

	// Grab the user from the database
	user, err := models.FindUserByID(db, uint(idInt))
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUserPromote", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return
	}
	if user.ID == dmrconst.ParrotUser {
		// Prevent promoting the Parrot user
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot promote the Parrot user"})
		return
	}
	if !user.Approved {
		// Prevent promoting an unapproved user
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot promote an unapproved user"})
		return
	}
	user.Admin = true
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "function", "POSTUserPromote", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User promoted"})

	if config.SMTP.Enabled {
		err = smtp.SendToAdmins(
			config,
			db,
			"Admin user promotion",
			fmt.Sprintf("An admin has been promoted.<br><br>Username: %s<br>Callsign: %s<br>DMR ID: %d", user.Username, strings.ToUpper(user.Callsign), user.ID),
		)
		if err != nil {
			slog.Error("Error sending email", "function", "POSTUserPromote", "error", err)
		}
	}
}

func POSTUserUnsuspend(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")

	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	session := sessions.Default(c)
	fromUserID, ok := session.Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgNotLoggedIn})
		return
	}
	if uint(userID) == fromUserID {
		// don't allow a user to demote themselves
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot unsuspend yourself"})
		return
	}

	// Grab the user from the database
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUserUnsuspend", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return
	}

	user.Suspended = false
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "function", "POSTUserUnsuspend", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User unsuspended"})
}

func POSTUserApprove(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")

	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	session := sessions.Default(c)
	fromUserID, ok := session.Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: msgNotLoggedIn})
		return
	}
	if uint(userID) == fromUserID {
		// don't allow a user to demote themselves
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot approve yourself"})
		return
	}

	// Grab the user from the database
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUserApprove", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return
	}

	user.Approved = true
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "function", "POSTUserApprove", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User approved"})
}

// userProfileResponse is the public-safe view of a user profile.
// It intentionally omits username, email, and password.
type userProfileResponse struct {
	ID        uint              `json:"id"`
	Callsign  string            `json:"callsign"`
	Admin     bool              `json:"admin"`
	Approved  bool              `json:"approved"`
	Repeaters []models.Repeater `json:"repeaters"`
	CreatedAt string            `json:"created_at"`
}

func GETUserProfile(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")
	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgUserNotFound})
		return
	}
	c.JSON(http.StatusOK, userProfileResponse{
		ID:        user.ID,
		Callsign:  user.Callsign,
		Admin:     user.Admin,
		Approved:  user.Approved,
		Repeaters: user.Repeaters,
		CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func GETUser(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")
	// Convert string id into uint
	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgUserNotFound})
		return
	}
	c.JSON(http.StatusOK, user)
}

func GETUserAdmins(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}

	users, err := models.FindUserAdmins(db)
	if err != nil {
		slog.Error("Error finding users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Admins not found"})
		return
	}

	total, err := models.CountUserAdmins(cDb)
	if err != nil {
		slog.Error("Error counting users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Admins not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{usersKey: users, totalKey: total})
}

func GETUserSuspended(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	// Get all users where approved = false
	users, err := models.FindUserSuspended(db)
	if err != nil {
		slog.Error("Error finding users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Suspended users not found"})
		return
	}
	total, err := models.CountUserSuspended(cDb)
	if err != nil {
		slog.Error("Error counting users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Suspended users not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{usersKey: users, totalKey: total})
}

func GETUserUnapproved(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	// Get all users where approved = false
	users, err := models.FindUserUnapproved(db)
	if err != nil {
		slog.Error("Error finding users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Unapproved users not found"})
		return
	}

	total, err := models.CountUserUnapproved(cDb)
	if err != nil {
		slog.Error("Error counting users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Unapproved users not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{usersKey: users, totalKey: total})
}

func PATCHUser(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	id := c.Param("id")
	idInt, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	var json apimodels.UserPatch
	err = c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "PATCHUser", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "JSON data is invalid"})
	} else {
		user, err := models.FindUserByID(db, uint(idInt))
		if err != nil {
			slog.Error(msgErrFindingUser, "error", err)
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgUserNotFound})
			return
		}

		if json.Callsign != "" {
			// Check DMR ID is in the database
			if userdb.ValidUserCallsign(user.ID, json.Callsign) {
				user.Callsign = strings.ToUpper(json.Callsign)
			} else {
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Callsign does not match DMR ID"})
				return
			}
		}

		if json.Username != "" {
			// Check if the username is already taken
			var existingUser models.User
			err := db.Find(&existingUser, "username = ?", json.Username).Error
			if err != nil {
				slog.Error(msgErrFindingUser, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
				return
			} else if existingUser.ID != 0 {
				c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username is already taken"})
				return
			}
			user.Username = json.Username
		}

		if json.Password != "" {
			hashedPassword, hashErr := utils.HashPassword(json.Password, config.PasswordSalt)
			if hashErr != nil {
				slog.Error("Error hashing password", "error", hashErr)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error hashing password"})
				return
			}
			user.Password = hashedPassword
		}

		err = db.Save(&user).Error
		if err != nil {
			slog.Error("Error updating user", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating user"})
			return
		}
		c.JSON(http.StatusOK, gin.H{messageKey: "User updated"})
	}
}

func DELETEUser(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid user ID"})
		return
	}

	exists, err := models.UserIDExists(db, uint(idUint64))
	if err != nil {
		slog.Error("Error checking if user exists", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error checking if user exists"})
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgUserNotFound})
		return
	}

	err = models.DeleteUser(db, uint(idUint64))
	if err != nil {
		slog.Error("Error deleting user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error deleting user"})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User deleted"})
}

func POSTUserSuspend(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")

	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidUserID})
		return
	}
	session := sessions.Default(c)
	fromUserID, ok := session.Get("user_id").(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}
	if uint(userID) == fromUserID {
		// don't allow a user to demote themselves
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot suspend yourself"})
		return
	}

	// Grab the user from the database
	user, err := models.FindUserByID(db, uint(userID))
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}

	if user.Admin || user.SuperAdmin {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot suspend an admin"})
		return
	}

	if user.ID == dmrconst.ParrotUser {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "You cannot suspend the Parrot user"})
		return
	}

	user.Suspended = true
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	c.JSON(http.StatusOK, gin.H{messageKey: "User suspended"})
}

func GETUserSelf(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{errorKey: "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}

	user, err := models.FindUserByID(db, uid)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}
	c.JSON(http.StatusOK, user)
}
