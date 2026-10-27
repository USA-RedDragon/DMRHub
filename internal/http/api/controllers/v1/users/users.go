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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	authctl "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/auth"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/sessionstore"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	usersKey            = "users"
	errorKey            = "error"
	totalKey            = "total"
	msgErrGettingUser   = "Error getting user"
	messageKey          = "message"
	msgInvalidUserID    = "Invalid User ID"
	msgErrDeletingUser  = "Error deleting user"
	msgNotLoggedIn      = "Not logged in"
	msgErrSavingUser    = "Error saving user"
	msgUserNotFound     = "User does not exist"
	msgErrFindingUser   = "Error finding user"
	msgCallsignMismatch = "Callsign does not match DMR ID"
	msgInvalidJSON      = "JSON data is invalid"
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
	user, ok := registration(c, db, config.DMR.DisableRadioIDValidation, config.HIBPAPIKey, config.PasswordSalt)
	if !ok {
		return
	}
	user.Approved = config.AutoApprove
	if err := db.Create(&user).Error; err != nil {
		slog.Error("Error creating user", "function", "POSTUser", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error creating user"})
		return
	}
	hubUserChanged(c, user.ID)
	authctl.SendEmailVerification(c, db, nil, user.ID, user.Callsign, user.Email)
	body := fmt.Sprintf("A new user has registered.\n\nUsername: %s\nCallsign: %s\nDMR ID: %d\n", user.Username, user.Callsign, user.ID)
	if config.AutoApprove {
		c.JSON(http.StatusOK, gin.H{messageKey: "Your account is ready. You can log in now."})
	} else {
		c.JSON(http.StatusOK, gin.H{messageKey: "User created, please wait for admin approval"})
		body += fmt.Sprintf("\nTo approve them, open %s/admin/users/approval\n", strings.TrimRight(config.HTTP.CanonicalHost, "/"))
	}
	if config.SMTP.Enabled {
		if err := smtp.SendToAdmins(config, db, "New user registration", body); err != nil {
			slog.Error("Error sending email", "function", "POSTUser", "error", err)
		}
	}
}

// POSTFirstAdmin makes the account in the request the first admin, while
// no admin exists. The first visitor to send it wins; every later one gets
// 409 Conflict.
func POSTFirstAdmin(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := registration(c, db, config.DMR.DisableRadioIDValidation, config.HIBPAPIKey, config.PasswordSalt)
	if !ok {
		return
	}
	if err := models.ClaimFirstAdmin(db, &user); err != nil {
		if errors.Is(err, models.ErrFirstAdminClaimed) {
			c.JSON(http.StatusConflict, gin.H{errorKey: "Someone else made the first admin already. Log in with that account."})
			return
		}
		slog.Error("Error creating the first admin", "function", "POSTFirstAdmin", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error creating user"})
		return
	}
	slog.Info("A visitor made the first admin", "username", user.Username, "id", user.ID)
	authctl.SendEmailVerification(c, db, nil, user.ID, user.Callsign, user.Email)
	c.JSON(http.StatusOK, gin.H{messageKey: "You are the admin of this DMRHub. You can log in now."})
}

// registration checks the registration in the request and returns the
// user to create, with the password hashed. On a bad registration it
// answers the request and returns false.
func registration(c *gin.Context, db *gorm.DB, skipRadioIDCheck bool, hibpKey, salt string) (models.User, bool) {
	var json apimodels.UserRegistration
	if err := c.ShouldBindJSON(&json); err != nil {
		slog.Error("JSON data is invalid", "function", "POSTUser", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
		return models.User{}, false
	}
	if !skipRadioIDCheck {
		if !userdb.IsValidUserID(json.DMRId) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "DMR ID is not valid"})
			return models.User{}, false
		}
		if !userdb.ValidUserCallsign(json.DMRId, json.Callsign) {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgCallsignMismatch})
			return models.User{}, false
		}
	}
	if isValid, errString := json.IsValidUsername(); !isValid {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: errString})
		return models.User{}, false
	}
	if json.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Password cannot be blank"})
		return models.User{}, false
	}
	email := strings.TrimSpace(json.Email)
	if !authctl.CheckEmail(c, email) {
		return models.User{}, false
	}
	taken, err := models.UsernameTaken(db, json.Username, 0)
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUser", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return models.User{}, false
	}
	if taken {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username is already taken"})
		return models.User{}, false
	}
	if !checkUsernameAndCallsign(c, db, json.DMRId, json.Username, json.Callsign) {
		return models.User{}, false
	}
	exists, err := models.UserIDExists(db, json.DMRId)
	if err == nil && !exists {
		_, exists, err = models.RadioIDOwner(db, json.DMRId)
	}
	if err != nil {
		slog.Error(msgErrGettingUser, "function", "POSTUser", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
		return models.User{}, false
	}
	if exists {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "DMR ID is already registered"})
		return models.User{}, false
	}
	if status, msg := utils.CheckPwnedPassword(c, hibpKey, json.Password); status != 0 {
		c.JSON(status, gin.H{errorKey: msg})
		return models.User{}, false
	}
	hashedPassword, err := utils.HashPassword(json.Password, salt)
	if err != nil {
		slog.Error("Error hashing password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error hashing password"})
		return models.User{}, false
	}
	return models.User{
		Username: json.Username,
		Password: hashedPassword,
		Callsign: strings.ToUpper(json.Callsign),
		ID:       json.DMRId,
		Email:    email,
	}, true
}

// checkUsernameAndCallsign refuses a username that is the callsign of a
// user other than id, and a callsign that is the username of a user other
// than id, both in any case. A login by a username wins over a login by a
// callsign, so either would take the logins of the other user. An empty
// username or callsign is not checked. On a refusal it answers the request
// and returns false.
func checkUsernameAndCallsign(c *gin.Context, db *gorm.DB, id uint, username, callsign string) bool {
	if username != "" {
		clash, err := models.UsernameIsCallsign(db, username, id)
		if err != nil {
			slog.Error(msgErrGettingUser, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
			return false
		}
		if clash {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username is the callsign of another user"})
			return false
		}
	}
	if callsign != "" {
		clash, err := models.UsernameTaken(db, callsign, id)
		if err != nil {
			slog.Error(msgErrGettingUser, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrGettingUser})
			return false
		}
		if clash {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Callsign is the username of another user"})
			return false
		}
	}
	return true
}

// checkPatchNames refuses a patch of user that sets a new username that is
// the callsign of another user, or a new callsign that is the username of
// another user. A name that only changes case is not new, so an edit that
// keeps an old clash is not refused.
func checkPatchNames(c *gin.Context, db *gorm.DB, user models.User, json apimodels.UserPatch) bool {
	var username, callsign string
	if !strings.EqualFold(json.Username, user.Username) {
		username = json.Username
	}
	if !strings.EqualFold(json.Callsign, user.Callsign) {
		callsign = json.Callsign
	}
	return checkUsernameAndCallsign(c, db, user.ID, username, callsign)
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
	publishUserChanged(c, user.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "User demoted"})

	if config.SMTP.Enabled {
		err = smtp.SendToAdmins(
			config,
			db,
			"Admin user demotion",
			fmt.Sprintf("An admin has been demoted.\n\nUsername: %s\nCallsign: %s\nDMR ID: %d\n", user.Username, strings.ToUpper(user.Callsign), user.ID),
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
	publishUserChanged(c, user.ID)
	c.JSON(http.StatusOK, gin.H{messageKey: "User promoted"})

	if config.SMTP.Enabled {
		err = smtp.SendToAdmins(
			config,
			db,
			"Admin user promotion",
			fmt.Sprintf("An admin has been promoted.\n\nUsername: %s\nCallsign: %s\nDMR ID: %d\n", user.Username, strings.ToUpper(user.Callsign), user.ID),
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
	publishUserChanged(c, user.ID)
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
	publishUserChanged(c, user.ID)
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
	c.JSON(http.StatusOK, userWithEmail{User: user, Email: user.Email, EmailVerified: user.EmailVerified})
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
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgInvalidJSON})
	} else {
		user, err := models.FindUserByID(db, uint(idInt))
		if err != nil {
			slog.Error(msgErrFindingUser, "error", err)
			c.JSON(http.StatusBadRequest, gin.H{errorKey: msgUserNotFound})
			return
		}

		if !checkPatchNames(c, db, user, json) {
			return
		}
		adminReset, ok := checkPasswordChange(c, db, json, user)
		if !ok {
			return
		}
		var email string
		if json.Email != nil {
			email = strings.TrimSpace(*json.Email)
			if !authctl.CheckEmail(c, email) {
				return
			}
		}

		override, ok := patchNames(c, db, !config.DMR.DisableRadioIDValidation, &user, json)
		if !ok {
			return
		}

		var hashedPassword string
		if json.Password != "" {
			if status, msg := utils.CheckPwnedPassword(c, config.HIBPAPIKey, json.Password); status != 0 {
				c.JSON(status, gin.H{errorKey: msg})
				return
			}
			var hashErr error
			hashedPassword, hashErr = utils.HashPassword(json.Password, config.PasswordSalt)
			if hashErr != nil {
				slog.Error("Error hashing password", "error", hashErr)
				c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error hashing password"})
				return
			}
		}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(&user).Error; err != nil {
				return fmt.Errorf("could not save user %d: %w", user.ID, err)
			}
			if override != nil {
				if err := models.SetCallsignOverride(tx, user.ID, *override); err != nil {
					return err
				}
			}
			if json.Email != nil && email != user.Email {
				if err := models.SetEmail(tx, user.ID, email); err != nil {
					return fmt.Errorf("could not set the email of user %d: %w", user.ID, err)
				}
			}
			if hashedPassword == "" {
				return nil
			}
			return models.SetPassword(tx, user.ID, hashedPassword, adminReset)
		})
		if err != nil {
			slog.Error("Error updating user", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error updating user"})
			return
		}
		if adminReset && !utils.RevokeUserGrants(c, user.ID) {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Password changed, but the apps of the user could not be signed out"})
			return
		}
		if json.Password != "" && !endOtherSessions(c, user.ID) {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Password changed, but the other sessions could not be ended"})
			return
		}
		if json.Email != nil && email != user.Email {
			authctl.SendEmailVerification(c, db, nil, user.ID, user.Callsign, email)
		}
		c.JSON(http.StatusOK, gin.H{messageKey: "User updated"})
	}
}

// patchNames applies the new callsign and username of json to user. It
// answers the request and returns false when one of them is refused. A new
// callsign gives override, which reports that an admin chose it against
// RadioID.
func patchNames(c *gin.Context, db *gorm.DB, radioID bool, user *models.User, json apimodels.UserPatch) (override *bool, ok bool) {
	if callsign := strings.ToUpper(strings.TrimSpace(json.Callsign)); callsign != "" && callsign != user.Callsign {
		o, ok := checkNewCallsign(c, db, radioID, user.ID, callsign, json.OverrideRadioID)
		if !ok {
			return nil, false
		}
		user.Callsign = callsign
		override = &o
	}
	if json.Username == "" || json.Username == user.Username {
		return override, true
	}
	if valid, msg := apimodels.IsValidUsername(json.Username); !valid {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msg})
		return nil, false
	}
	taken, err := models.UsernameTaken(db, json.Username, user.ID)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return nil, false
	}
	if taken {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Username is already taken"})
		return nil, false
	}
	user.Username = json.Username
	return override, true
}

// checkNewCallsign answers the request and returns false when the user
// id must not get the new callsign: another user has it, or radioID
// is set and RadioID gives the DMR ID another callsign. An admin who sets
// override gets the callsign anyway, and override is then true. An admin
// who does not set it learns the callsign that RadioID gives, to override
// it on purpose.
func checkNewCallsign(c *gin.Context, db *gorm.DB, radioID bool, id uint, callsign string, wantOverride bool) (override bool, ok bool) {
	taken, err := models.UsernameIsCallsign(db, callsign, id)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return false, false
	}
	if taken {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Callsign is already taken"})
		return false, false
	}
	if !radioID || userdb.ValidUserCallsign(id, callsign) {
		return false, true
	}
	actorID, _ := sessions.Default(c).Get("user_id").(uint)
	actor, err := models.FindUserWithoutRepeaters(db, actorID)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return false, false
	}
	if !actor.Admin && !actor.SuperAdmin {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: msgCallsignMismatch})
		return false, false
	}
	if wantOverride {
		return true, true
	}
	var known string
	if entry, found := userdb.Get(id); found {
		known = strings.ToUpper(entry.Callsign)
	}
	c.JSON(http.StatusBadRequest, gin.H{errorKey: msgCallsignMismatch, "radio_id_mismatch": true, "radio_id_callsign": known})
	return false, false
}

// checkPasswordChange answers the request and returns false when the
// PATCH of user must not go on: a user with a temporary password who
// does not change it, a change of one's own password without the right
// current one or to the same one, or a reset of an admin by an admin who
// is not the System Admin. adminReset reports a password that an admin
// sets for another user.
func checkPasswordChange(c *gin.Context, db *gorm.DB, json apimodels.UserPatch, user models.User) (adminReset bool, ok bool) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return false, false
	}
	actorID, _ := sessions.Default(c).Get("user_id").(uint)
	if json.Password == "" && actorID == user.ID && user.MustChangePassword {
		c.JSON(http.StatusForbidden, gin.H{errorKey: middleware.MsgMustChangePassword, "must_change_password": true})
		return false, false
	}
	if json.Password != "" && actorID == user.ID {
		if json.CurrentPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Enter your current password."})
			return false, false
		}
		verified, verifyErr := utils.VerifyPassword(json.CurrentPassword, user.Password, config.PasswordSalt)
		if verifyErr != nil || !verified {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "The current password is wrong."})
			return false, false
		}
		if json.Password == json.CurrentPassword {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "The new password must not be the current one."})
			return false, false
		}
	}
	adminReset = json.Password != "" && actorID != user.ID
	if adminReset && (user.Admin || user.SuperAdmin) {
		actor, err := models.FindUserByID(db, actorID)
		if err != nil {
			slog.Error(msgErrFindingUser, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
			return false, false
		}
		if !actor.SuperAdmin {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "Only the System Admin can reset the password of an admin."})
			return false, false
		}
	}
	return adminReset, true
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

	repeaters, err := models.GetUserRepeaters(db, uint(idUint64))
	if err != nil {
		slog.Error("Error listing the repeaters of a user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrDeletingUser})
		return
	}

	peers, err := models.GetUserPeers(db, uint(idUint64))
	if err != nil {
		slog.Error("Error listing the peers of a user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrDeletingUser})
		return
	}

	err = models.DeleteUser(db, uint(idUint64))
	if err != nil {
		slog.Error("Error deleting user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrDeletingUser})
		return
	}
	if dmrHub, ok := c.MustGet("Hub").(*hub.Hub); ok && dmrHub != nil {
		for _, r := range repeaters {
			dmrHub.ReloadRepeater(c.Request.Context(), r.ID)
		}
		for _, p := range peers {
			dmrHub.PeerChanged(p.ID)
		}
	}
	if store, ok := c.Value("SessionStore").(*sessionstore.Store); ok {
		if err := store.DeleteUserSessions(c.Request.Context(), uint(idUint64), ""); err != nil {
			slog.Error("Failed to end the old sessions of a deleted user", "user", idUint64, "error", err)
		}
	}
	publishUserChanged(c, uint(idUint64))
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

	user.Suspended = true
	err = db.Save(&user).Error
	if err != nil {
		slog.Error(msgErrSavingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrSavingUser})
		return
	}
	publishUserChanged(c, user.ID)
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
	tracking, err := models.TracksPositions(db, uid)
	if err != nil {
		slog.Error(msgErrFindingUser, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgErrFindingUser})
		return
	}
	self := userSelf{User: user, SMSStorage: user.SMSStorageEnabled, PositionTracking: tracking}
	g, viaToken := utils.Grant(c)
	if !viaToken || g.Allows(oauth.ScopeHotspotsRead) || g.Allows(oauth.ScopeHotspotsTalkgroups) {
		self.Repeaters = &user.Repeaters
	}
	if !viaToken {
		self.Email, self.EmailVerified = &user.Email, &user.EmailVerified
	}
	c.JSON(http.StatusOK, self)
}

// userSelf is the logged-in user, with whether SMS Storage and position
// tracking are on, so that an app can tell why the inbox or the map is
// empty. It leaves out the repeaters for an app without a hotspot scope.
type userSelf struct {
	models.User
	Repeaters        *[]models.Repeater `json:"repeaters,omitempty"`
	SMSStorage       bool               `json:"sms_storage"`
	PositionTracking bool               `json:"position_tracking"`
	// Email and EmailVerified are left out for an app.
	Email         *string `json:"email,omitempty"`
	EmailVerified *bool   `json:"email_verified,omitempty"`
}

// userWithEmail is a user with the email address, for the user and admins.
type userWithEmail struct {
	models.User
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func publishUserChanged(c *gin.Context, userID uint) {
	hubUserChanged(c, userID)
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a changed user stay open", "user", userID)
		return
	}
	if err := websocket.PublishUserChanged(ps, userID); err != nil {
		slog.Error("Failed to close the websockets of a changed user", "user", userID, "error", err)
	}
}

func hubUserChanged(c *gin.Context, userID uint) {
	if v, ok := c.Get("Hub"); ok {
		if h, ok := v.(*hub.Hub); ok && h != nil {
			h.UserChanged(userID)
		}
	}
}

func endOtherSessions(c *gin.Context, userID uint) bool {
	sessionID := sessions.Default(c).ID()
	store, ok := c.Value("SessionStore").(*sessionstore.Store)
	if !ok {
		slog.Error("SessionStore not found in context", "user", userID)
		return false
	}
	if err := store.DeleteUserSessions(c.Request.Context(), userID, sessionID); err != nil {
		slog.Error("Failed to end the other sessions of a user", "user", userID, "error", err)
		return false
	}
	ps, ok := c.Value("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub not found in context, the websockets of a user stay open", "user", userID)
		return false
	}
	if err := websocket.PublishPasswordChanged(ps, userID, sessionID); err != nil {
		slog.Error("Failed to close the websockets of a user", "user", userID, "error", err)
		return false
	}
	return true
}
