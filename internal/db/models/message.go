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
	"time"

	"gorm.io/gorm"
)

// MessageFormat is the format of a stored data message.
type MessageFormat string

// The MessageFormat values. MessageFormatData is a data message that is not
// text.
const (
	MessageFormatTMS         MessageFormat = "tms"
	MessageFormatIP          MessageFormat = "ip"
	MessageFormatAnyTone     MessageFormat = "anytone"
	MessageFormatDefinedData MessageFormat = "defined_data"
	MessageFormatHytera      MessageFormat = "hytera"
	MessageFormatData        MessageFormat = "data"
	MessageFormatUnknown     MessageFormat = "unknown"
)

// Message is one data message that the hub routed: an SMS in any format,
// or a data message that is not text. The hub stores every message,
// whatever the SMS Storage setting of its users. Raw is the 12-octet data
// header and then the user data, as received.
type Message struct {
	ID             uint      `gorm:"primarykey"`
	ReceivedAt     time.Time `gorm:"not null;index;index:idx_messages_destination_received,priority:2;index:idx_messages_source_received,priority:2"`
	TransmissionID *uint     `gorm:"index"`
	Transmission   *Call     `gorm:"foreignKey:TransmissionID;constraint:OnDelete:SET NULL"`
	SourceID       uint      `gorm:"not null;index:idx_messages_source_received,priority:1"`
	DestinationID  uint      `gorm:"not null;index:idx_messages_destination_received,priority:1"`
	IsGroup        bool      `gorm:"not null;default:false"`
	RepeaterID     uint      `gorm:"not null;default:0"`
	Slot           uint8     `gorm:"not null;default:1"`
	Transport      string    `gorm:"size:16"`
	StreamID       uint      `gorm:"not null;default:0"`
	SAP            *uint8
	Format         MessageFormat `gorm:"size:16;not null;index"`
	Confirmed      bool          `gorm:"not null;default:false"`
	SendSequence   *uint8
	FragmentSeq    *uint8
	Response       CallResponse `gorm:"size:8"`
	BadCRC         bool         `gorm:"not null;default:false"`
	Text           *string      `gorm:"type:text"`
	ChecksumOK     bool         `gorm:"not null"`
	Blocks         uint         `gorm:"not null;default:0"`
	Octets         uint         `gorm:"not null;default:0"`
	Raw            []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName returns the table of the messages.
func (Message) TableName() string {
	return "messages"
}

// BeforeSave keeps ReceivedAt in UTC, so that SQLite, which compares times
// as text, orders and filters them right.
func (m *Message) BeforeSave(*gorm.DB) error {
	m.ReceivedAt = m.ReceivedAt.UTC()
	return nil
}

// UserRadioID is a subscriber radio ID that a user owns besides the
// account ID, User.ID. VerifiedAt is when the hub checked the ID against
// the user database, or when an admin added it.
type UserRadioID struct {
	ID         uint      `gorm:"primarykey"`
	UserID     uint      `gorm:"not null;index"`
	User       *User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	RadioID    uint      `gorm:"not null;uniqueIndex"`
	VerifiedAt time.Time `gorm:"not null"`
	CreatedAt  time.Time
}

// TableName returns the table of the owned radio IDs.
func (UserRadioID) TableName() string {
	return "user_radio_ids"
}

// BeforeSave keeps VerifiedAt in UTC.
func (r *UserRadioID) BeforeSave(*gorm.DB) error {
	r.VerifiedAt = r.VerifiedAt.UTC()
	return nil
}
