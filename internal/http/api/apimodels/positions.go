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

package apimodels

import (
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// Position is one reported position of a radio.
type Position struct {
	RadioID    uint       `json:"radio_id"`
	Callsign   string     `json:"callsign,omitempty"`
	Shared     bool       `json:"shared"`
	Latitude   float64    `json:"latitude"`
	Longitude  float64    `json:"longitude"`
	Format     string     `json:"format"`
	AccuracyM  *float64   `json:"accuracy_m"`
	SpeedMPS   *float64   `json:"speed_mps"`
	HeadingDeg *float64   `json:"heading_deg"`
	AltitudeM  *float64   `json:"altitude_m"`
	FixTime    *time.Time `json:"fix_time"`
	ReceivedAt time.Time  `json:"received_at"`
	RepeaterID uint       `json:"repeater_id"`
	Slot       uint8      `json:"slot"`
}

// NewPosition returns the API form of p.
func NewPosition(p *models.Position) Position {
	return Position{
		RadioID: p.RadioID, Latitude: p.Latitude, Longitude: p.Longitude, Format: string(p.Format),
		AccuracyM: p.AccuracyM, SpeedMPS: p.SpeedMPS, HeadingDeg: p.HeadingDeg, AltitudeM: p.AltitudeM,
		FixTime: p.FixTime, ReceivedAt: p.ReceivedAt, RepeaterID: p.RepeaterID, Slot: p.Slot,
	}
}

// PositionList is the newest position of each radio that the user sees.
type PositionList struct {
	Positions []Position `json:"positions"`
}

// RadioPositions is the newest position and the track of one radio.
type RadioPositions struct {
	Latest *Position  `json:"latest"`
	Track  []Position `json:"track"`
}

// PositionRadio is the position setting of one radio ID of the user.
type PositionRadio struct {
	RadioID         uint   `json:"radio_id"`
	Tracking        bool   `json:"tracking"`
	IntervalMinutes uint   `json:"interval_minutes"`
	RetentionHours  uint   `json:"retention_hours"`
	APRS            bool   `json:"aprs"`
	APRSSSID        string `json:"aprs_ssid"`
	APRSSymbol      string `json:"aprs_symbol"`
	APRSComment     string `json:"aprs_comment"`
}

// NewPositionRadio returns the API form of s.
func NewPositionRadio(s *models.PositionSetting) PositionRadio {
	return PositionRadio{
		RadioID: s.RadioID, Tracking: s.Tracking, IntervalMinutes: s.IntervalSeconds / 60, RetentionHours: s.RetentionHours,
		APRS: s.APRS, APRSSSID: s.APRSSSID, APRSSymbol: s.APRSSymbol, APRSComment: s.APRSComment,
	}
}

// PositionRadios is the position settings of the radio IDs of the user and
// the choices the hub allows.
type PositionRadios struct {
	Radios    []PositionRadio `json:"radios"`
	Intervals []uint          `json:"intervals"`
	Retention []uint          `json:"retention_hours"`
	APRS      bool            `json:"aprs"`
	Callsign  string          `json:"callsign"`
}

// PositionRadioPut sets the position setting of a radio ID.
type PositionRadioPut struct {
	Tracking        *bool  `json:"tracking" binding:"required"`
	IntervalMinutes uint   `json:"interval_minutes" binding:"required"`
	RetentionHours  uint   `json:"retention_hours" binding:"required"`
	APRS            bool   `json:"aprs"`
	APRSSSID        string `json:"aprs_ssid"`
	APRSSymbol      string `json:"aprs_symbol"`
	APRSComment     string `json:"aprs_comment"`
}

// PositionPushKind is the kind of a position frame on /ws/messages.
const PositionPushKind = "position"

// PositionPush is a new position, pushed to each user who sees it.
type PositionPush struct {
	Kind     string   `json:"kind"`
	Position Position `json:"position"`
}
