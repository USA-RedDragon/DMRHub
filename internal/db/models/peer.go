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

package models

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Peer is the model for an OpenBridge DMR peer
//
//go:generate go run github.com/tinylib/msgp
type Peer struct {
	ID        uint           `json:"id" gorm:"primaryKey" msg:"id"`
	LastPing  time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP        string         `json:"ip" msg:"ip"`
	Port      int            `json:"port" msg:"port"`
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
		slog.Error("Failed to marshal peer to json", "error", err)
		return ""
	}
	return string(jsn)
}

func ListPeers(db *gorm.DB) ([]Peer, error) {
	var peers []Peer
	err := db.Preload("Owner").Order("id asc").Find(&peers).Error
	return peers, err
}

func CountPeers(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Peer{}).Count(&count).Error
	return int(count), err
}

func GetUserPeers(db *gorm.DB, id uint) ([]Peer, error) {
	var peers []Peer
	err := db.Preload("Owner").Where("owner_id = ?", id).Order("id asc").Find(&peers).Error
	return peers, err
}

func CountUserPeers(db *gorm.DB, id uint) (int, error) {
	var count int64
	err := db.Model(&Peer{}).Where("owner_id = ?", id).Count(&count).Error
	return int(count), err
}

func FindPeerByID(db *gorm.DB, id uint) (Peer, error) {
	var peer Peer
	err := db.Preload("Owner").First(&peer, id).Error
	return peer, err
}

func PeerIDExists(db *gorm.DB, id uint) (bool, error) {
	var count int64
	err := db.Model(&Peer{}).Where("id = ?", id).Limit(1).Count(&count).Error
	return count > 0, err
}

func DeletePeer(db *gorm.DB, id uint) error {
	if err := db.Unscoped().Delete(&Peer{ID: id}).Error; err != nil {
		return fmt.Errorf("delete peer: %w", err)
	}
	return nil
}
