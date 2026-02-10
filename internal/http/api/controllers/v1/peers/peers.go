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

package peers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

func GETPeers(c *gin.Context) {
	db, ok := c.MustGet("PaginatedDB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
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
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	cDb, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	session := sessions.Default(c)

	userID := session.Get("user_id")
	if userID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		slog.Error("Unable to convert userID to uint", "function", "GETMyPeers", "userID", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	// Get all peers owned by user
	peers := models.GetUserPeers(db, uid)
	if db.Error != nil {
		slog.Error("Error getting peers owned by user", "userID", userID, "error", db.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting peers owned by user"})
		return
	}

	count := models.CountUserPeers(cDb, uid)

	c.JSON(http.StatusOK, gin.H{"total": count, "peers": peers})
}

func GETPeer(c *gin.Context) {
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("Unable to get DB from context")
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
		slog.Error("Unable to get DB from context")
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
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	userID, ok := usID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
	}
	db, ok := c.MustGet("DB").(*gorm.DB)
	if !ok {
		slog.Error("DB cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	config, ok := c.MustGet("Config").(*config.Config)
	if !ok {
		slog.Error("Unable to get Config from context")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	pubsub, ok := c.MustGet("PubSub").(pubsub.PubSub)
	if !ok {
		slog.Error("PubSub cast failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	var json apimodels.PeerPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTPeer", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		if models.PeerIDExists(db, json.ID) {
			slog.Error("Peer ID already exists", "function", "POSTPeer", "peerID", json.ID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Peer ID already exists"})
			return
		}

		var peer models.Peer

		peer.Egress = json.Egress
		peer.Ingress = json.Ingress

		// Peer validated to fit within a 4 byte integer
		if json.ID <= 0 || json.ID > 4294967295 {
			slog.Error("Peer ID is invalid", "function", "POSTPeer", "peerID", json.ID)
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
			slog.Error("Failed to generate a peer password", "function", "POSTPeer", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to generate a peer password"})
			return
		}

		var user models.User
		db.First(&user, json.OwnerID)
		if db.Error != nil {
			slog.Error("Error getting user", "function", "POSTPeer", "userID", userID, "error", db.Error)
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
		go openbridge.GetSubscriptionManager().Subscribe(c.Request.Context(), pubsub, peer)

		if config.SMTP.Enabled {
			err = smtp.SendToAdmins(
				config,
				db,
				"OpenBridge peer created",
				"New OpenBridge peer created with ID "+strconv.FormatUint(uint64(peer.ID), 10)+" by "+peer.Owner.Username,
			)
			if err != nil {
				slog.Error("Error sending email", "function", "POSTPeer", "error", err)
			}
		}
	}
}
