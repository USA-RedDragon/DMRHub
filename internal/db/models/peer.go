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

package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/klog/v2"
)

// Peer is the model for an OpenBridge DMR peer
//
//go:generate msgp
type Peer struct {
	ID            uint           `json:"id" gorm:"primaryKey" msg:"id"`
	Connection    string         `json:"-" gorm:"-" msg:"connection"`
	Connected     time.Time      `json:"connected_time" msg:"connected"`
	PingsReceived uint           `json:"-" gorm:"-" msg:"pings_received"`
	LastPing      time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP            string         `json:"-" gorm:"-" msg:"ip"`
	Port          int            `json:"-" gorm:"-" msg:"port"`
	Password      string         `json:"-" msg:"-"`
	Owner         User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID       uint           `json:"-" msg:"-"`
	CreatedAt     time.Time      `json:"created_at" msg:"-"`
	UpdatedAt     time.Time      `json:"-" msg:"-"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

func (p *Peer) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		klog.Errorf("Failed to marshal peer to json: %s", err)
		return ""
	}
	return string(jsn)
}

func ListPeers(db *gorm.DB) []Peer {
	var peers []Peer
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Order("id asc").Find(&peers)
	return peers
}

func CountPeers(db *gorm.DB) int {
	var count int64
	db.Model(&Peer{}).Count(&count)
	return int(count)
}

func GetUserPeers(db *gorm.DB, id uint) []Peer {
	var peers []Peer
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Where("owner_id = ?", id).Order("id asc").Find(&peers)
	return peers
}

func CountUserPeers(db *gorm.DB, id uint) int {
	var count int64
	db.Model(&Peer{}).Where("owner_id = ?", id).Count(&count)
	return int(count)
}

func FindPeerByID(db *gorm.DB, id uint) Peer {
	var peer Peer
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").First(&peer, id)
	return peer
}

func PeerExists(db *gorm.DB, repeater Repeater) bool {
	var count int64
	db.Model(&Repeater{}).Where("id = ?", repeater.RadioID).Limit(1).Count(&count)
	return count > 0
}

func PeerIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&Repeater{}).Where("id = ?", id).Limit(1).Count(&count)
	return count > 0
}

func DeletePeer(db *gorm.DB, id uint) {
	err := db.Transaction(func(tx *gorm.DB) error {
		tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, id, id).Delete(&Call{})
		tx.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(&Repeater{RadioID: id})
		return nil
	})
	if err != nil {
		klog.Errorf("Error deleting repeater: %s", err)
	}
}
