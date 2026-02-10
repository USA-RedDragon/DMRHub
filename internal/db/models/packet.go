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
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
)

// Packet is a DMR packet
//
//go:generate go run github.com/tinylib/msgp
type Packet struct {
	Signature   string             `msg:"signature"`
	Seq         uint               `msg:"seq"`
	Src         uint               `msg:"src"`
	Dst         uint               `msg:"dst"`
	Repeater    uint               `msg:"repeater"`
	Slot        bool               `msg:"slot"`
	GroupCall   bool               `msg:"groupCall"`
	FrameType   dmrconst.FrameType `msg:"frameType,extension"`
	DTypeOrVSeq uint               `msg:"dtypeOrVSeq"`
	StreamID    uint               `msg:"streamID"`
	DMRData     [33]byte           `msg:"dmrData"`
	// The next two are technically unsigned, but the data type is 1 byte
	// We also want to be able to represent -1 as a null, so we use int
	BER  int `msg:"ber"`
	RSSI int `msg:"rssi"`
}

func (p Packet) Equal(other Packet) bool {
	if p.Signature != other.Signature {
		return false
	}
	if p.Seq != other.Seq {
		return false
	}
	if p.Src != other.Src {
		return false
	}
	if p.Dst != other.Dst {
		return false
	}
	if p.Repeater != other.Repeater {
		return false
	}
	if p.Slot != other.Slot {
		return false
	}
	if p.GroupCall != other.GroupCall {
		return false
	}
	if p.FrameType != other.FrameType {
		return false
	}
	if p.DTypeOrVSeq != other.DTypeOrVSeq {
		return false
	}
	if p.StreamID != other.StreamID {
		return false
	}
	if p.DMRData != other.DMRData {
		return false
	}
	if p.BER != other.BER {
		return false
	}
	if p.RSSI != other.RSSI {
		return false
	}
	return true
}

func UnpackPacket(data []byte) (Packet, bool) {
	var packet Packet
	if len(data) < dmrconst.MMDVMPacketLength {
		return packet, false
	}
	if len(data) > dmrconst.MMDVMMaxPacketLength {
		return packet, false
	}
	packet.Signature = string(data[:4])
	packet.Seq = uint(data[4])
	packet.Src = uint(data[5])<<16 | uint(data[6])<<8 | uint(data[7])
	packet.Dst = uint(data[8])<<16 | uint(data[9])<<8 | uint(data[10])
	packet.Repeater = uint(data[11])<<24 | uint(data[12])<<16 | uint(data[13])<<8 | uint(data[14])
	bits := data[15]
	packet.Slot = false
	if (bits & 0x80) != 0 {
		packet.Slot = true
	}
	packet.GroupCall = true
	if (bits & 0x40) != 0 {
		packet.GroupCall = false
	}
	packet.FrameType = dmrconst.FrameType((bits & 0x30) >> 4)
	packet.DTypeOrVSeq = uint(bits & 0xF)
	packet.StreamID = uint(data[16])<<24 | uint(data[17])<<16 | uint(data[18])<<8 | uint(data[19])
	copy(packet.DMRData[:], data[20:53])
	// Bytes 53-54 are BER and RSSI, respectively
	// But they are optional, so don't error if they don't exist
	if len(data) > dmrconst.MMDVMPacketLength {
		packet.BER = int(data[dmrconst.MMDVMPacketLength])
	} else {
		packet.BER = -1
	}
	if len(data) > dmrconst.MMDVMPacketLength+1 {
		packet.RSSI = int(data[dmrconst.MMDVMPacketLength+1])
	} else {
		packet.RSSI = -1
	}
	return packet, true
}

func (p *Packet) String() string {
	return fmt.Sprintf(
		"Packet: Seq %d, Src %d, Dst %d, Repeater %d, Slot %t, GroupCall %t, FrameType=%s, DTypeOrVSeq %d, StreamId %d, BER %d, RSSI %d, DMRData %v",
		p.Seq, p.Src, p.Dst, p.Repeater, p.Slot, p.GroupCall, p.FrameType.String(), p.DTypeOrVSeq, p.StreamID, p.BER, p.RSSI, p.DMRData,
	)
}

func (p *Packet) Encode() []byte {
	// Encode the packet as we decoded
	data := make([]byte, dmrconst.MMDVMPacketLength)
	copy(data[:4], []byte(p.Signature))
	data[4] = byte(p.Seq)
	data[5] = byte(p.Src >> 16)
	data[6] = byte(p.Src >> 8)
	data[7] = byte(p.Src)
	data[8] = byte(p.Dst >> 16)
	data[9] = byte(p.Dst >> 8)
	data[10] = byte(p.Dst)
	data[11] = byte(p.Repeater >> 24)
	data[12] = byte(p.Repeater >> 16)
	data[13] = byte(p.Repeater >> 8)
	data[14] = byte(p.Repeater)
	bits := byte(0)
	if p.Slot {
		bits |= 0x80
	}
	if !p.GroupCall {
		bits |= 0x40
	}
	bits |= byte(p.FrameType << 4)
	bits |= byte(p.DTypeOrVSeq)
	data[15] = bits
	data[16] = byte(p.StreamID >> 24)
	data[17] = byte(p.StreamID >> 16)
	data[18] = byte(p.StreamID >> 8)
	data[19] = byte(p.StreamID)
	copy(data[20:53], p.DMRData[:])
	// If BER and RSSI are set, add them
	if p.BER != -1 && p.RSSI != -1 {
		data = append(data, byte(p.BER))
		data = append(data, byte(p.RSSI))
	}
	return data
}
