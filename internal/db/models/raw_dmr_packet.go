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
	"net"
	"time"
)

// RawDMRPacket is a raw DMR packet
//
//go:generate go run github.com/tinylib/msgp
type RawDMRPacket struct {
	Data       []byte `msg:"data"`
	RemoteIP   string `msg:"remote_ip"`
	RemotePort int    `msg:"remote_port"`
	// TrackID is the Packet.TrackID of the packet in Data.
	TrackID uint64 `msg:"track_id"`
	// Codec, Copy and HasCopy are those of the Packet in Data.
	Codec   uint8 `msg:"codec"`
	Copy    bool  `msg:"copy"`
	HasCopy bool  `msg:"has_copy"`
	// Transport names the transport of a packet that a server received or
	// sends on another transport than its UDP socket, and Addr is its
	// address there. Neither is encoded.
	Transport string   `msg:"-"`
	Addr      net.Addr `msg:"-"`
	// ReceivedAt is when the server read the packet. It is not encoded.
	ReceivedAt time.Time `msg:"-"`
}
