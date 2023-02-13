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

// Repeater is the model for a DMR repeater
//
//go:generate msgp
type Repeater struct {
	RadioID               uint           `json:"id" gorm:"primaryKey" msg:"radio_id"`
	Connection            string         `json:"-" gorm:"-" msg:"connection"`
	Connected             time.Time      `json:"connected_time" msg:"connected"`
	PingsReceived         uint           `json:"-" gorm:"-" msg:"pings_received"`
	LastPing              time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP                    string         `json:"-" gorm:"-" msg:"ip"`
	Port                  int            `json:"-" gorm:"-" msg:"port"`
	Salt                  uint32         `json:"-" gorm:"-" msg:"salt"`
	Callsign              string         `json:"callsign" msg:"callsign"`
	RXFrequency           uint           `json:"rx_frequency" msg:"rx_frequency"`
	TXFrequency           uint           `json:"tx_frequency" msg:"tx_frequency"`
	TXPower               uint           `json:"tx_power" msg:"tx_power"`
	ColorCode             uint           `json:"color_code" msg:"color_code"`
	Latitude              float32        `json:"latitude" msg:"latitude"`
	Longitude             float32        `json:"longitude" msg:"longitude"`
	Height                int            `json:"height" msg:"height"`
	Location              string         `json:"location" msg:"location"`
	Description           string         `json:"description" msg:"description"`
	Slots                 uint           `json:"slots" msg:"slots"`
	URL                   string         `json:"url" msg:"url"`
	SoftwareID            string         `json:"software_id" msg:"software_id"`
	PackageID             string         `json:"package_id" msg:"package_id"`
	Password              string         `json:"-" msg:"-"`
	TS1StaticTalkgroups   []Talkgroup    `json:"ts1_static_talkgroups" gorm:"many2many:repeater_ts1_static_talkgroups;" msg:"-"`
	TS2StaticTalkgroups   []Talkgroup    `json:"ts2_static_talkgroups" gorm:"many2many:repeater_ts2_static_talkgroups;" msg:"-"`
	TS1DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS2DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS1DynamicTalkgroup   Talkgroup      `json:"ts1_dynamic_talkgroup" gorm:"foreignKey:TS1DynamicTalkgroupID" msg:"-"`
	TS2DynamicTalkgroup   Talkgroup      `json:"ts2_dynamic_talkgroup" gorm:"foreignKey:TS2DynamicTalkgroupID" msg:"-"`
	Owner                 User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID               uint           `json:"-" msg:"-"`
	Hotspot               bool           `json:"hotspot" msg:"hotspot"`
	CreatedAt             time.Time      `json:"created_at" msg:"-"`
	UpdatedAt             time.Time      `json:"-" msg:"-"`
	DeletedAt             gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

func (p *Repeater) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		klog.Errorf("Failed to marshal repeater to json: %s", err)
		return ""
	}
	return string(jsn)
}

func ListRepeaters(db *gorm.DB) []Repeater {
	var repeaters []Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Order("radio_id asc").Find(&repeaters)
	return repeaters
}

func CountRepeaters(db *gorm.DB) int {
	var count int64
	db.Model(&Repeater{}).Count(&count)
	return int(count)
}

func GetUserRepeaters(db *gorm.DB, id uint) []Repeater {
	var repeaters []Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Where("owner_id = ?", id).Order("radio_id asc").Find(&repeaters)
	return repeaters
}

func CountUserRepeaters(db *gorm.DB, id uint) int {
	var count int64
	db.Model(&Repeater{}).Where("owner_id = ?", id).Count(&count)
	return int(count)
}

func FindRepeaterByID(db *gorm.DB, id uint) Repeater {
	var repeater Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").First(&repeater, id)
	return repeater
}

func RepeaterExists(db *gorm.DB, repeater Repeater) bool {
	var count int64
	db.Model(&Repeater{}).Where("radio_id = ?", repeater.RadioID).Limit(1).Count(&count)
	return count > 0
}

func RepeaterIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&Repeater{}).Where("radio_id = ?", id).Limit(1).Count(&count)
	return count > 0
}

func DeleteRepeater(db *gorm.DB, id uint) {
	err := db.Transaction(func(tx *gorm.DB) error {
		tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, id, id).Delete(&Call{})
		tx.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(&Repeater{RadioID: id})
		return nil
	})
	if err != nil {
		klog.Errorf("Error deleting repeater: %s", err)
	}
}

func (p *Repeater) WantRX(packet Packet) (bool, bool) {
	if packet.Dst == p.RadioID {
		return true, packet.Slot
	}

	if p.TS2DynamicTalkgroupID != nil {
		if packet.Dst == *p.TS2DynamicTalkgroupID {
			return true, true
		}
	}

	if p.TS1DynamicTalkgroupID != nil {
		if packet.Dst == *p.TS1DynamicTalkgroupID {
			return true, false
		}
	}

	if p.InTS2StaticTalkgroups(packet.Dst) {
		return true, true
	} else if p.InTS1StaticTalkgroups(packet.Dst) {
		return true, false
	}

	return false, false
}

func (p *Repeater) InTS2StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS2StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}

func (p *Repeater) InTS1StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS1StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}
