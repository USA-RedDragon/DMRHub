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

package peers

import (
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

func GETPeers(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	peers := models.ListPeers(db)
	count := models.CountPeers(cDb)
	c.JSON(http.StatusOK, gin.H{"total": count, "peers": peers})
}

func GETMyPeers(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		logging.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		logging.Errorf("Unable to convert userID to uint: %v", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Get all peers owned by user
	peers := models.GetUserPeers(db, uid)
	if db.Error != nil {
		logging.Errorf("Error getting peers owned by user %d: %v", userID, db.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting peers owned by user"})
		return
	}

	count := models.CountUserPeers(cDb, uid)

	c.JSON(http.StatusOK, gin.H{"total": count, "peers": peers})
}

func GETPeer(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	id := c.Param("id")
	// Convert string id into uint
	peerID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer ID"})
		return
	}
	if models.PeerIDExists(db, uint(peerID)) {
		peer := models.FindPeerByID(db, uint(peerID))
		c.JSON(http.StatusOK, peer)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Peer does not exist"})
	}
}

func DELETEPeer(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Errorf("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer ID"})
		return
	}
	models.DeletePeer(db, uint(idUint64))
	if db.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Peer deleted"})
}

func POSTPeer(c *gin.Context) {
	session := sessions.Default(c)
	usID := session.Get("user_id")
	if usID == nil {
		logging.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	userID, ok := usID.(uint)
	if !ok {
		logging.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		logging.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	redis, ok := c.MustGet("Redis").(*redis.Client)
	if !ok {
		logging.Error("Redis cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	var json apimodels.PeerPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		logging.Errorf("POSTPeer: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if models.PeerIDExists(db, json.ID) {
			logging.Errorf("POSTPeer: Peer ID already exists: %v", json.ID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Peer ID already exists"})
			return
		}

		var peer models.Peer

		peer.Egress = json.Egress
		peer.Ingress = json.Ingress

		// Peer validated to fit within a 4 byte integer
		if json.ID <= 0 || json.ID > 4294967295 {
			logging.Errorf("POSTPeer: Peer ID is invalid: %v", json.ID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Peer ID is invalid"})
			return
		}

		peer.ID = json.ID

		// Generate a random password of 12 characters
		const randLen = 12
		const randNum = 1
		const randSpecial = 2
		peer.Password, err = utils.RandomPassword(randLen, randNum, randSpecial)
		if err != nil {
			logging.Errorf("Failed to generate a peer password %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to generate a peer password"})
			return
		}

		var user models.User
		db.First(&user, json.OwnerID)
		if db.Error != nil {
			logging.Errorf("Error getting user %d: %v", userID, db.Error)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting user"})
			return
		}

		// Find user by userID
		peer.Owner = user
		peer.OwnerID = json.OwnerID
		db.Preload("Owner").Create(&peer)
		if db.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": db.Error.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Peer created", "password": peer.Password})
		go openbridge.GetSubscriptionManager().Subscribe(c.Request.Context(), redis, peer)

		if config.GetConfig().EnableEmail {
			smtp.Send(
				config.GetConfig().AdminEmail,
				"OpenBridge peer created",
				"New OpenBridge peer created with ID "+strconv.FormatUint(uint64(peer.ID), 10)+" by "+peer.Owner.Username,
			)
		}
	}
}
