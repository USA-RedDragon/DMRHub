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
	"fmt"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
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

// UnpackPacket decodes a DMRD packet of 53, 55 or 71 octets. A packet
// without the BER and RSSI octets gets -1 in both.
func UnpackPacket(data []byte) (Packet, bool) {
	d, err := dmrd.DecodeData(data)
	if err != nil {
		return Packet{}, false
	}
	return PacketFromData(&d, len(data) > dmrconst.MMDVMPacketLength), true
}

// PacketFromData returns the Packet of the DMRD packet d. hasQuality
// reports whether d carried the BER and RSSI octets; without them both are
// -1.
func PacketFromData(d *dmrd.Data, hasQuality bool) Packet {
	p := Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         uint(d.Seq),
		Src:         uint(d.Source),
		Dst:         uint(d.Destination),
		Repeater:    uint(d.Repeater),
		Slot:        d.Slot == enums.Slot2,
		GroupCall:   !d.Private,
		FrameType:   dmrconst.FrameType(d.Frame),
		DTypeOrVSeq: uint(d.Value),
		StreamID:    uint(d.Stream),
		DMRData:     d.Burst,
		BER:         -1,
		RSSI:        -1,
	}
	if hasQuality {
		p.BER, p.RSSI = int(d.BER), int(d.RSSI)
	}
	return p
}

func (p *Packet) String() string {
	return fmt.Sprintf(
		"Packet: Seq %d, Src %d, Dst %d, Repeater %d, Slot %t, GroupCall %t, FrameType=%s, DTypeOrVSeq %d, StreamId %d, BER %d, RSSI %d, DMRData %v",
		p.Seq, p.Src, p.Dst, p.Repeater, p.Slot, p.GroupCall, p.FrameType.String(), p.DTypeOrVSeq, p.StreamID, p.BER, p.RSSI, p.DMRData,
	)
}

// Data returns p as a 55-octet DMRD packet. Each field is cut to its width
// in the packet, and a BER or RSSI of -1 becomes 0.
func (p *Packet) Data() dmrd.Data {
	if p.BER == -1 {
		p.BER = 0
	}
	if p.RSSI == -1 {
		p.RSSI = 0
	}
	slot := enums.Slot1
	if p.Slot {
		slot = enums.Slot2
	}
	return dmrd.Data{
		Seq:         uint8(p.Seq),
		Source:      uint32(p.Src & dmrconst.MaxDMRAddress),
		Destination: uint32(p.Dst & dmrconst.MaxDMRAddress),
		Repeater:    uint32(p.Repeater),
		Slot:        slot,
		Private:     !p.GroupCall,
		Frame:       dmrd.FrameType(p.FrameType & 0x3),
		Value:       uint8(p.DTypeOrVSeq & 0xF),
		Stream:      uint32(p.StreamID),
		Burst:       p.DMRData,
		BER:         uint8(p.BER),
		RSSI:        uint8(p.RSSI),
		Size:        dmrconst.MMDVMMaxPacketLength,
	}
}

// EncodeTo encodes the packet into dst, which must be at least dmrconst.MMDVMMaxPacketLength bytes.
// Returns dst for convenience.
func (p *Packet) EncodeTo(dst []byte) []byte {
	d := p.Data()
	data, err := d.AppendBinary(dst[:0])
	if err != nil {
		slog.Error("Failed to encode DMRD packet", "error", err)
	}
	return data
}

// Encode allocates and returns a new byte slice containing the encoded packet.
func (p *Packet) Encode() []byte {
	data := make([]byte, dmrconst.MMDVMMaxPacketLength)
	p.EncodeTo(data)
	return data
}
