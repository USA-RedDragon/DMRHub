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

package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/google/go-cmp/cmp"
)

//nolint:gochecknoglobals
var knownGoodPacketBytes = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 165, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 7, 8}

//nolint:gochecknoglobals
var knownGoodPacket = models.Packet{
	Signature:   "DMRD",
	Seq:         1,
	Src:         2,
	Dst:         3,
	Repeater:    4,
	Slot:        true,
	GroupCall:   true,
	FrameType:   dmrconst.FrameDataSync,
	DTypeOrVSeq: 5,
	StreamID:    6,
	BER:         7,
	RSSI:        8,
	DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
}

func TestDecode(t *testing.T) {
	t.Parallel()
	packet, ok := models.UnpackPacket(knownGoodPacketBytes)
	if !ok {
		t.Errorf("Packet did not decode properly")
	}
	if !cmp.Equal(knownGoodPacket, packet) {
		t.Errorf("Packet did not decode properly")
	}
	t.Log(knownGoodPacket.String())
}

func FuzzDecode(f *testing.F) {
	f.Fuzz(func(t *testing.T, a []byte) {
		t.Parallel()
		models.UnpackPacket(a)
	})
}

func FuzzEncode(f *testing.F) {
	f.Fuzz(func(t *testing.T, signature string, seq uint, src uint, dst uint, repeater uint, slot bool, groupCall bool, frameType uint, dtypeOrVSeq uint, streamID uint, ber int, rssi int, dmrData []byte) {
		t.Parallel()
		dmrByteData := [33]byte{}
		// DMRData needs to be a [33]byte
		if len(dmrData) > 33 {
			copy(dmrByteData[:], dmrData[:33])
		} else if len(dmrData) < 33 {
			// fill dmrByteData with dmrData and then fill the rest with 0s
			copy(dmrByteData[:], dmrData)
			for i := len(dmrData); i < 33; i++ {
				dmrByteData[i] = 0
			}
		}

		packet := models.Packet{
			Signature:   signature,
			Seq:         seq,
			Src:         src,
			Dst:         dst,
			Repeater:    repeater,
			Slot:        slot,
			GroupCall:   groupCall,
			FrameType:   dmrconst.FrameType(frameType),
			DTypeOrVSeq: dtypeOrVSeq,
			StreamID:    streamID,
			BER:         ber,
			RSSI:        rssi,
			DMRData:     dmrByteData,
		}
		packetBytes := packet.Encode()
		if len(packetBytes) != dmrconst.MMDVMPacketLength && len(packetBytes) != dmrconst.MMDVMMaxPacketLength {
			t.Errorf("Packet did not encode properly. Length was %d", len(packetBytes))
		}
	})
}

func TestEncode(t *testing.T) {
	t.Parallel()
	if !cmp.Equal(knownGoodPacketBytes, knownGoodPacket.Encode()) {
		t.Errorf("Packet did not encode properly")
	}
}

//nolint:gochecknoglobals
var knownGoodPacketBytesNoRSSIandBER = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 165, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33}

func TestPacketWithoutRSSIandBERIsNegOne(t *testing.T) {
	t.Parallel()
	p, ok := models.UnpackPacket(knownGoodPacketBytesNoRSSIandBER)
	if !ok || p.RSSI != -1 || p.BER != -1 {
		t.Errorf("Packet did not decode properly")
	}
}

//nolint:gochecknoglobals
var knownGoodPacketPrivate = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 229, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 7, 8}

//nolint:gochecknoglobals
var knownGoodPrivatePacket = models.Packet{
	Signature:   "DMRD",
	Seq:         1,
	Src:         2,
	Dst:         3,
	Repeater:    4,
	Slot:        true,
	GroupCall:   false,
	FrameType:   dmrconst.FrameDataSync,
	DTypeOrVSeq: 5,
	StreamID:    6,
	BER:         7,
	RSSI:        8,
	DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
}

func TestPacketPrivateDecode(t *testing.T) {
	t.Parallel()
	p, ok := models.UnpackPacket(knownGoodPacketPrivate)
	if p.GroupCall || !ok {
		t.Errorf("Packet did not decode properly")
	}
}

func TestPacketPrivateEncode(t *testing.T) {
	t.Parallel()
	if !cmp.Equal(knownGoodPrivatePacket.Encode(), knownGoodPacketPrivate) {
		t.Errorf("Packet did not encode properly")
	}
}

func BenchmarkEncodeHomebrewPacket(b *testing.B) {
	b.StopTimer()
	p := models.Packet{}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		p.Encode()
	}
}

func BenchmarkDecodeHomebrewPacket(b *testing.B) {
	b.StopTimer()
	p := models.Packet{}
	bytes := p.Encode()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		models.UnpackPacket(bytes)
	}
}

func TestEqualDifferentSignature(t *testing.T) {
	t.Parallel()
	packet1 := models.Packet{
		Signature:   "DMRD",
		Seq:         1,
		Src:         2,
		Dst:         3,
		Repeater:    4,
		Slot:        true,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 5,
		StreamID:    6,
		BER:         7,
		RSSI:        8,
		DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
	}
	packet2 := packet1
	packet2.Signature = "DIFF"
	if packet1.Equal(packet2) {
		t.Errorf("Expected packets to be different due to different Signature")
	}
}

func TestEqualDifferentPackets(t *testing.T) {
	t.Parallel()
	packet1 := models.Packet{
		Signature:   "DMRD",
		Seq:         1,
		Src:         2,
		Dst:         3,
		Repeater:    4,
		Slot:        true,
		GroupCall:   true,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 5,
		StreamID:    6,
		BER:         7,
		RSSI:        8,
		DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
	}
	packet2 := models.Packet{
		Signature:   "DMRD",
		Seq:         2,
		Src:         3,
		Dst:         4,
		Repeater:    5,
		Slot:        false,
		GroupCall:   false,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 6,
		StreamID:    7,
		BER:         8,
		RSSI:        9,
		DMRData:     [33]byte{33, 32, 31, 30, 29, 28, 27, 26, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
	}
	if packet1.Equal(packet2) {
		t.Errorf("Expected packets to be different")
	}
}
