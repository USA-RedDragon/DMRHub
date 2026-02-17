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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
)

func GETPeerRules(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	peerID, err := strconv.ParseUint(c.Param("id"), 10, 32)
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Peer not found"})
		return
	}

	rules, err := models.ListRulesForPeer(db, uint(peerID))
	if err != nil {
		slog.Error("Error listing peer rules", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

func POSTPeerRule(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	peerID, err := strconv.ParseUint(c.Param("id"), 10, 32)
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Peer not found"})
		return
	}

	var json apimodels.PeerRulePost
	if err := c.ShouldBindJSON(&json); err != nil {
		slog.Error("JSON data is invalid", "function", "POSTPeerRule", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
		return
	}

	if json.SubjectIDMin > json.SubjectIDMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subject_id_min must be <= subject_id_max"})
		return
	}

	rule := models.PeerRule{
		PeerID:       uint(peerID),
		Direction:    json.Direction,
		SubjectIDMin: json.SubjectIDMin,
		SubjectIDMax: json.SubjectIDMax,
	}
	if err := db.Create(&rule).Error; err != nil {
		slog.Error("Error creating peer rule", "peerID", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Peer rule created", "rule": rule})
}

func DELETEPeerRule(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	peerID, err := strconv.ParseUint(c.Param("id"), 10, 32)
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Peer not found"})
		return
	}

	ruleID, err := strconv.ParseUint(c.Param("ruleId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	rule, err := models.FindPeerRuleByID(db, uint(ruleID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found"})
		return
	}

	if rule.PeerID != uint(peerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Rule does not belong to this peer"})
		return
	}

	if err := models.DeletePeerRule(db, uint(ruleID)); err != nil {
		slog.Error("Error deleting peer rule", "ruleID", ruleID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Try again later"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Peer rule deleted"})
}
