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

const (
	// RepeaterTypeMMDVM is the type for MMDVM repeaters.
	RepeaterTypeMMDVM = "mmdvm"
	// RepeaterTypeIPSC is the type for Motorola IPSC repeaters.
	RepeaterTypeIPSC = "ipsc"
)

// Repeater is the model for a DMR repeater
//
//go:generate go run github.com/tinylib/msgp
type Repeater struct {
	Connection            string         `json:"-" gorm:"-" msg:"connection"`
	Connected             time.Time      `json:"connected_time" msg:"connected"`
	PingsReceived         uint           `json:"-" gorm:"-" msg:"pings_received"`
	LastPing              time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP                    string         `json:"-" gorm:"-" msg:"ip"`
	Port                  int            `json:"-" gorm:"-" msg:"port"`
	Salt                  uint32         `json:"-" gorm:"-" msg:"salt"`
	Password              string         `json:"-" msg:"-"`
	Type                  string         `json:"type" gorm:"default:'mmdvm'" msg:"-"`
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
	RepeaterConfiguration
}

func (p *Repeater) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		slog.Error("Failed to marshal repeater to json", "error", err)
		return ""
	}
	return string(jsn)
}

func ListRepeaters(db *gorm.DB) ([]Repeater, error) {
	var repeaters []Repeater
	err := db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Order("id asc").Find(&repeaters).Error
	return repeaters, err
}

func CountRepeaters(db *gorm.DB) (int, error) {
	var count int64
	err := db.Model(&Repeater{}).Count(&count).Error
	return int(count), err
}

func GetUserRepeaters(db *gorm.DB, id uint) ([]Repeater, error) {
	var repeaters []Repeater
	err := db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Where("owner_id = ?", id).Order("id asc").Find(&repeaters).Error
	return repeaters, err
}

func CountUserRepeaters(db *gorm.DB, id uint) (int, error) {
	var count int64
	err := db.Model(&Repeater{}).Where("owner_id = ?", id).Count(&count).Error
	return int(count), err
}

func FindRepeaterByID(db *gorm.DB, id uint) (Repeater, error) {
	var repeater Repeater
	err := db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").First(&repeater, id).Error
	return repeater, err
}

func RepeaterExists(db *gorm.DB, repeater Repeater) (bool, error) {
	var count int64
	err := db.Model(&Repeater{}).Where("id = ?", repeater.ID).Limit(1).Count(&count).Error
	return count > 0, err
}

func RepeaterIDExists(db *gorm.DB, id uint) (bool, error) {
	var count int64
	err := db.Model(&Repeater{}).Where("id = ?", id).Limit(1).Count(&count).Error
	return count > 0, err
}

func DeleteRepeater(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, id, id).Delete(&Call{})
		tx.Unscoped().Where("id = ?", id).Select("TS1StaticTalkgroups", "TS2StaticTalkgroups").Delete(&Repeater{})
		return nil
	})
	if err != nil {
		return fmt.Errorf("error deleting repeater: %w", err)
	}
	return nil
}

func (p *Repeater) UpdateFrom(repeater Repeater) {
	p.Connected = repeater.Connected
	p.LastPing = repeater.LastPing
	p.Callsign = repeater.Callsign
	p.RXFrequency = repeater.RXFrequency
	p.TXFrequency = repeater.TXFrequency
	p.TXPower = repeater.TXPower
	p.ColorCode = repeater.ColorCode
	p.Latitude = repeater.Latitude
	p.Longitude = repeater.Longitude
	p.Height = repeater.Height
	p.Location = repeater.Location
	p.Description = repeater.Description
	p.Slots = repeater.Slots
	p.URL = repeater.URL
	p.SoftwareID = repeater.SoftwareID
	p.PackageID = repeater.PackageID
}

func (p *Repeater) WantRX(packet Packet) (bool, bool) {
	if packet.Dst == p.ID {
		return true, packet.Slot
	}

	if packet.Dst == p.OwnerID {
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

func (p *Repeater) WantRXCall(call Call) (bool, bool) {
	if call.DestinationID == p.ID {
		return true, call.TimeSlot
	}

	if call.DestinationID == p.OwnerID {
		return true, call.TimeSlot
	}

	if p.TS2DynamicTalkgroupID != nil {
		if call.DestinationID == *p.TS2DynamicTalkgroupID {
			return true, true
		}
	}

	if p.TS1DynamicTalkgroupID != nil {
		if call.DestinationID == *p.TS1DynamicTalkgroupID {
			return true, false
		}
	}

	if p.InTS2StaticTalkgroups(call.DestinationID) {
		return true, true
	} else if p.InTS1StaticTalkgroups(call.DestinationID) {
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
