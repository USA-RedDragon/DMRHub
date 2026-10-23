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

import "time"

// Message is a stored data message as a user sees it.
type Message struct {
	ID                  uint      `json:"id"`
	ReceivedAt          time.Time `json:"received_at"`
	SourceID            uint      `json:"source_id"`
	SourceCallsign      string    `json:"source_callsign"`
	DestinationID       uint      `json:"destination_id"`
	DestinationCallsign string    `json:"destination_callsign"`
	Group               bool      `json:"group"`
	TalkgroupName       string    `json:"talkgroup_name"`
	Format              string    `json:"format"`
	Text                *string   `json:"text"`
	Confirmed           bool      `json:"confirmed"`
	Response            string    `json:"response"`
	ChecksumOK          bool      `json:"checksum_ok"`
	Octets              uint      `json:"octets"`
}

// AdminMessage is a stored data message as an admin sees it. Raw, in hex,
// is set only for a single message.
type AdminMessage struct {
	Message
	TransmissionID *uint   `json:"transmission_id"`
	RepeaterID     uint    `json:"repeater_id"`
	Slot           uint8   `json:"slot"`
	Transport      string  `json:"transport"`
	StreamID       uint    `json:"stream_id"`
	SAP            *uint8  `json:"sap"`
	BadCRC         bool    `json:"bad_crc"`
	Blocks         uint    `json:"blocks"`
	Raw            *string `json:"raw,omitempty"`
}

// MessageList is a page of a user's inbox or outbox.
type MessageList struct {
	Total    int       `json:"total"`
	Messages []Message `json:"messages"`
}

// AdminMessageList is a page of an admin search.
type AdminMessageList struct {
	Total    int64          `json:"total"`
	Messages []AdminMessage `json:"messages"`
}

// MessageDay is the number of messages of one day.
type MessageDay struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// MessageStats counts the messages of a user's inbox and outbox.
type MessageStats struct {
	Inbox    int            `json:"inbox"`
	Outbox   int            `json:"outbox"`
	ACK      int            `json:"ack"`
	NACK     int            `json:"nack"`
	ByFormat map[string]int `json:"by_format"`
	ByDay    []MessageDay   `json:"by_day"`
}

// MessagePush is a websocket frame of a new or changed message.
type MessagePush struct {
	Boxes   []string `json:"boxes"`
	Message Message  `json:"message"`
}
