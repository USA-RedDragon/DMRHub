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
	"net"
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	LinkTypeDynamic = "dynamic"
	LinkTypeStatic  = "static"
)

func GETPeers(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
		return
	}
	peers, err := models.ListPeers(db)
	if err != nil {
		slog.Error("Error listing peers", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	count, err := models.CountPeers(cDb)
	if err != nil {
		slog.Error("Error counting peers", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": count, "peers": peers})
}

func GETMyPeers(c *gin.Context) {
	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		return
	}
	cDb, ok := utils.GetDB(c)
	if !ok {
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
	peers, err := models.GetUserPeers(db, uid)
	if err != nil {
		slog.Error("Error getting peers owned by user", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting peers owned by user"})
		return
	}

	count, err := models.CountUserPeers(cDb, uid)
	if err != nil {
		slog.Error("Error counting user peers", "userID", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count, "peers": peers})
}

func GETPeer(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	id := c.Param("id")
	// Convert string id into uint
	peerID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer ID"})
		return
	}
	exists, err := models.PeerIDExists(db, uint(peerID))
	if err != nil {
		slog.Error("Error checking peer existence", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Peer does not exist"})
		return
	}
	peer, err := models.FindPeerByID(db, uint(peerID))
	if err != nil {
		slog.Error("Error finding peer", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusOK, peer)
}

func DELETEPeer(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer ID"})
		return
	}
	if err = models.DeletePeer(db, uint(idUint64)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Peer deleted"})
}

func PATCHPeer(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	idUint64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer ID"})
		return
	}
	peerID := uint(idUint64)
	exists, err := models.PeerIDExists(db, peerID)
	if err != nil {
		slog.Error("Error checking peer existence", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Peer not found"})
		return
	}

	var json apimodels.PeerPatch
	if err := c.ShouldBindJSON(&json); err != nil {
		slog.Error("JSON data is invalid", "function", "PATCHPeer", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	}

	updates := make(map[string]interface{})
	if json.IP != nil {
		if net.ParseIP(*json.IP) == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
			return
		}
		updates["ip"] = *json.IP
	}
	if json.Port != nil {
		const maxPort = 65535
		if *json.Port < 1 || *json.Port > maxPort {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Port must be between 1 and 65535"})
			return
		}
		updates["port"] = *json.Port
	}
	if json.Ingress != nil {
		updates["ingress"] = *json.Ingress
	}
	if json.Egress != nil {
		updates["egress"] = *json.Egress
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	if err := db.Model(&models.Peer{}).Where("id = ?", peerID).Updates(updates).Error; err != nil {
		slog.Error("Error updating peer", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	peer, err := models.FindPeerByID(db, peerID)
	if err != nil {
		slog.Error("Error finding updated peer", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusOK, peer)
}

func POSTPeer(c *gin.Context) {
	session := sessions.Default(c)
	usID := session.Get("user_id")
	if usID == nil {
		slog.Error("userID not found")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}
	userID, ok := usID.(uint)
	if !ok {
		slog.Error("userID cast failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	var json apimodels.PeerPost
	err := c.ShouldBindJSON(&json)
	if err != nil {
		slog.Error("JSON data is invalid", "function", "POSTPeer", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		exists, existsErr := models.PeerIDExists(db, json.ID)
		if existsErr != nil {
			slog.Error("Error checking peer ID existence", "function", "POSTPeer", "peerID", json.ID, "error", existsErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
			return
		}
		if exists {
			slog.Error("Peer ID already exists", "function", "POSTPeer", "peerID", json.ID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Peer ID already exists"})
			return
		}

		// Validate IP address
		if net.ParseIP(json.IP) == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
			return
		}

		// Validate port range
		const maxPort = 65535
		if json.Port < 1 || json.Port > maxPort {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Port must be between 1 and 65535"})
			return
		}

		var peer models.Peer

		peer.Egress = json.Egress
		peer.Ingress = json.Ingress
		peer.IP = json.IP
		peer.Port = json.Port

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
