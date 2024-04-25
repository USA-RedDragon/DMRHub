// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package models

import (
	"encoding/json"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"gorm.io/gorm"
)

// Peer is the model for an OpenBridge DMR peer
//
//go:generate go run github.com/tinylib/msgp
type Peer struct {
	ID        uint           `json:"id" gorm:"primaryKey" msg:"id"`
	LastPing  time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP        string         `json:"-" gorm:"-" msg:"ip"`
	Port      int            `json:"-" gorm:"-" msg:"port"`
	Password  string         `json:"-" msg:"-"`
	Owner     User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID   uint           `json:"-" msg:"-"`
	Ingress   bool           `json:"ingress" msg:"-"`
	Egress    bool           `json:"egress" msg:"-"`
	CreatedAt time.Time      `json:"created_at" msg:"-"`
	UpdatedAt time.Time      `json:"-" msg:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

func (p *Peer) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		logging.Errorf("Failed to marshal peer to json: %s", err)
		return ""
	}
	return string(jsn)
}

func ListPeers(db *gorm.DB) []Peer {
	var peers []Peer
	db.Preload("Owner").Order("id asc").Find(&peers)
	return peers
}

func CountPeers(db *gorm.DB) int {
	var count int64
	db.Model(&Peer{}).Count(&count)
	return int(count)
}

func GetUserPeers(db *gorm.DB, id uint) []Peer {
	var peers []Peer
	db.Preload("Owner").Where("owner_id = ?", id).Order("id asc").Find(&peers)
	return peers
}

func CountUserPeers(db *gorm.DB, id uint) int {
	var count int64
	db.Model(&Peer{}).Where("owner_id = ?", id).Count(&count)
	return int(count)
}

func FindPeerByID(db *gorm.DB, id uint) Peer {
	var peer Peer
	db.Preload("Owner").First(&peer, id)
	return peer
}

func PeerExists(db *gorm.DB, peer Peer) bool {
	var count int64
	db.Model(&Peer{}).Where("id = ?", peer.ID).Limit(1).Count(&count)
	return count > 0
}

func PeerIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&Peer{}).Where("id = ?", id).Limit(1).Count(&count)
	return count > 0
}

func DeletePeer(db *gorm.DB, id uint) {
	tx := db.Unscoped().Delete(&Peer{ID: id})
	if tx.Error != nil {
		logging.Errorf("Error deleting repeater: %s", tx.Error)
	}
}
