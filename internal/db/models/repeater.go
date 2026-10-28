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
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	// RepeaterTypeMMDVM is the type for MMDVM repeaters.
	RepeaterTypeMMDVM = "mmdvm"
	// RepeaterTypeIPSC is the type for Motorola IPSC repeaters.
	RepeaterTypeIPSC = "ipsc"
	// RepeaterTypeModem is the type for the MMDVM modem board that DMRHub
	// drives over a serial port.
	RepeaterTypeModem = "modem"
)

type RepeaterState string

// The codec settings of a repeater timeslot, and the codec names.
const (
	CodecSettingAuto = "auto"
	CodecAMBE        = "ambe"
	CodecCodec2      = "codec2"
)

// ValidCodecSetting reports whether s is a codec setting of a timeslot.
func ValidCodecSetting(s string) bool {
	return s == CodecSettingAuto || s == CodecAMBE || s == CodecCodec2
}

// SlotCodec returns the codec that the radios on a timeslot of the
// repeater use, CodecAMBE or CodecCodec2: the setting of the slot, or with
// CodecSettingAuto the codec last heard on it. A slot is AMBE until it
// hears a Codec2 call, and IPSC repeaters are always AMBE. slot is true
// for TS2.
func (p *Repeater) SlotCodec(slot bool) string {
	if p.Type == RepeaterTypeIPSC {
		return CodecAMBE
	}
	setting, heard := p.TS1Codec, p.TS1HeardCodec
	if slot {
		setting, heard = p.TS2Codec, p.TS2HeardCodec
	}
	switch setting {
	case CodecAMBE, CodecCodec2:
		return setting
	}
	if heard == CodecCodec2 {
		return CodecCodec2
	}
	return CodecAMBE
}

// SlotCodec2Mode returns the Codec2 mode last heard on a timeslot, 0 when
// none was. slot is true for TS2.
func (p *Repeater) SlotCodec2Mode(slot bool) int {
	if slot {
		return p.TS2HeardCodec2Mode
	}
	return p.TS1HeardCodec2Mode
}

const (
	RepeaterStateConnected     RepeaterState = "CONNECTED"
	RepeaterStateChallengeSent RepeaterState = "CHALLENGE_SENT"
	RepeaterStateWaitingConfig RepeaterState = "WAITING_CONFIG"
	RepeaterStateLoginReceived RepeaterState = "LOGIN_RECEIVED"
)

// Repeater is the model for a DMR repeater
//
//go:generate go run github.com/tinylib/msgp
type Repeater struct {
	Connection            RepeaterState  `json:"-" gorm:"-" msg:"connection"`
	Connected             time.Time      `json:"connected_time" msg:"connected"`
	PingsReceived         uint           `json:"-" gorm:"-" msg:"pings_received"`
	LastPing              time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP                    string         `json:"-" gorm:"-" msg:"ip"`
	Port                  int            `json:"-" gorm:"-" msg:"port"`
	Salt                  uint32         `json:"-" gorm:"-" msg:"salt"`
	SecretCheck           []byte         `json:"-" gorm:"-" msg:"secret_check"`
	Password              string         `json:"-" gorm:"<-:create" msg:"-"`
	Type                  string         `json:"type" gorm:"default:'mmdvm'" msg:"-"`
	Dialect               string         `json:"dialect" msg:"dialect"`
	TS1StaticTalkgroups   []Talkgroup    `json:"ts1_static_talkgroups" gorm:"many2many:repeater_ts1_static_talkgroups;" msg:"-"`
	TS2StaticTalkgroups   []Talkgroup    `json:"ts2_static_talkgroups" gorm:"many2many:repeater_ts2_static_talkgroups;" msg:"-"`
	TS1DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS2DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS1DynamicTalkgroup   Talkgroup      `json:"ts1_dynamic_talkgroup" gorm:"foreignKey:TS1DynamicTalkgroupID" msg:"-"`
	TS2DynamicTalkgroup   Talkgroup      `json:"ts2_dynamic_talkgroup" gorm:"foreignKey:TS2DynamicTalkgroupID" msg:"-"`
	Owner                 User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID               uint           `json:"-" msg:"-"`
	SimplexRepeater       bool           `json:"simplex_repeater" msg:"simplex_repeater"`
	Trusted               bool           `json:"trusted" gorm:"default:false;<-:create" msg:"-"`
	City                  string         `json:"city" msg:"-"`
	State                 string         `json:"state" msg:"-"`
	Country               string         `json:"country" msg:"-"`
	CreatedAt             time.Time      `json:"created_at" msg:"-"`
	UpdatedAt             time.Time      `json:"-" msg:"-"`
	DeletedAt             gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
	RepeaterConfiguration

	// TS1Codec and TS2Codec are the codec setting of each timeslot:
	// CodecSettingAuto follows the last call heard on it. Like the heard
	// codec below, a save of the whole repeater does not write them, so a
	// stale copy cannot undo a change; SetRepeaterCodec and
	// SetRepeaterHeardCodec do.
	TS1Codec string `json:"ts1_codec" gorm:"size:8;not null;default:'auto';<-:create" msg:"-"`
	TS2Codec string `json:"ts2_codec" gorm:"size:8;not null;default:'auto';<-:create" msg:"-"`
	// TS1HeardCodec and TS2HeardCodec are the codec of the last call that
	// started with a Voice LC Header on each timeslot, "" before the
	// first. The Codec2 mode is the last one heard, and stays through
	// AMBE calls.
	TS1HeardCodec      string     `json:"ts1_heard_codec" gorm:"size:8;not null;default:'';<-:create" msg:"-"`
	TS2HeardCodec      string     `json:"ts2_heard_codec" gorm:"size:8;not null;default:'';<-:create" msg:"-"`
	TS1HeardCodec2Mode int        `json:"ts1_heard_codec2_mode" gorm:"not null;default:0;<-:create" msg:"-"`
	TS2HeardCodec2Mode int        `json:"ts2_heard_codec2_mode" gorm:"not null;default:0;<-:create" msg:"-"`
	TS1HeardAt         *time.Time `json:"ts1_heard_at" gorm:"<-:create" msg:"-"`
	TS2HeardAt         *time.Time `json:"ts2_heard_at" gorm:"<-:create" msg:"-"`
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

// SetRepeaterPassword replaces the password, or the IPSC auth key, of the
// repeater id. A save of the whole repeater does not write the password, so
// a stale copy cannot bring back the old one. It returns gorm.ErrRecordNotFound when there is no such
// repeater.
func SetRepeaterPassword(db *gorm.DB, id uint, password string) error {
	res := db.Exec("UPDATE repeaters SET password = ? WHERE id = ? AND deleted_at IS NULL", password, id)
	if res.Error != nil {
		return fmt.Errorf("failed to save the password of repeater %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteRepeater(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, id, id).Delete(&Call{}).Error; err != nil {
			return fmt.Errorf("delete calls: %w", err)
		}

		if err := tx.Where("repeater_id = ?", id).Delete(&RoutingRule{}).Error; err != nil {
			return fmt.Errorf("delete routing rules: %w", err)
		}

		repeater := Repeater{RepeaterConfiguration: RepeaterConfiguration{ID: id}}
		if err := tx.Model(&repeater).Association("TS1StaticTalkgroups").Clear(); err != nil {
			return fmt.Errorf("clear ts1 static talkgroups: %w", err)
		}
		if err := tx.Model(&repeater).Association("TS2StaticTalkgroups").Clear(); err != nil {
			return fmt.Errorf("clear ts2 static talkgroups: %w", err)
		}

		if err := tx.Unscoped().Where("id = ?", id).Delete(&Repeater{}).Error; err != nil {
			return fmt.Errorf("delete repeater: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error deleting repeater: %w", err)
	}
	return nil
}

// SetRepeaterStaticTalkgroups replaces the static talkgroups of the
// repeater id on TS1 with ts1 and on TS2 with ts2, in one transaction. A nil
// list leaves its slot as it is, and an empty one clears it. Each talkgroup
// must exist.
func SetRepeaterStaticTalkgroups(db *gorm.DB, id uint, ts1, ts2 []Talkgroup) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		repeater := Repeater{RepeaterConfiguration: RepeaterConfiguration{ID: id}}
		for _, slot := range []struct {
			name string
			tgs  []Talkgroup
		}{{"TS1StaticTalkgroups", ts1}, {"TS2StaticTalkgroups", ts2}} {
			switch {
			case slot.tgs == nil:
			case len(slot.tgs) == 0:
				if err := tx.Model(&repeater).Association(slot.name).Clear(); err != nil {
					return fmt.Errorf("clear %s: %w", slot.name, err)
				}
			default:
				if err := tx.Model(&repeater).Association(slot.name).Replace(slot.tgs); err != nil {
					return fmt.Errorf("replace %s: %w", slot.name, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error setting static talkgroups of repeater %d: %w", id, err)
	}
	return nil
}

func (p *Repeater) UpdateFrom(repeater Repeater) {
	p.Connected = repeater.Connected
	p.LastPing = repeater.LastPing
	p.Dialect = repeater.Dialect
	p.Callsign = repeater.Callsign
	p.RXFrequency = repeater.RXFrequency
	p.TXFrequency = repeater.TXFrequency
	p.TXPower = repeater.TXPower
	p.ColorCode = repeater.ColorCode
	p.Latitude = repeater.Latitude
	p.Longitude = repeater.Longitude
	p.Height = repeater.Height
	p.City = repeater.City
	p.State = repeater.State
	p.Country = repeater.Country
	p.Location = repeater.Location
	if parts := nonEmpty(repeater.City, repeater.State, repeater.Country); len(parts) > 0 {
		p.Location = strings.Join(parts, ", ")
	}
	p.Description = repeater.Description
	p.Slots = repeater.Slots
	p.URL = repeater.URL
	p.SoftwareID = repeater.SoftwareID
	p.PackageID = repeater.PackageID
}

func nonEmpty(values ...string) []string {
	out := values[:0:0]
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (p *Repeater) WantRX(packet Packet) (bool, bool) {
	return p.wantRX(packet.Dst, packet.Slot)
}

func (p *Repeater) WantRXCall(call Call) (bool, bool) {
	return p.wantRX(call.DestinationID, call.TimeSlot)
}

// wantRX is the shared implementation for WantRX and WantRXCall.
// It returns (wantPacket, slot) for a given destination ID and original slot.
func (p *Repeater) wantRX(destID uint, slot bool) (bool, bool) {
	if destID == p.ID {
		return true, slot
	}

	if destID == p.OwnerID {
		return true, slot
	}

	if p.TS2DynamicTalkgroupID != nil {
		if destID == *p.TS2DynamicTalkgroupID {
			return true, true
		}
	}

	if p.TS1DynamicTalkgroupID != nil {
		if destID == *p.TS1DynamicTalkgroupID {
			return true, false
		}
	}

	if isInStaticTalkgroups(p.TS2StaticTalkgroups, destID) {
		return true, true
	} else if isInStaticTalkgroups(p.TS1StaticTalkgroups, destID) {
		return true, false
	}

	return false, false
}

// isInStaticTalkgroups checks if a destination ID is in the given talkgroup slice.
func isInStaticTalkgroups(talkgroups []Talkgroup, dest uint) bool {
	for _, tg := range talkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}
