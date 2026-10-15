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

package ipsc

import (
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/dmr/enums"
	"github.com/USA-RedDragon/dmrgo/dmr/fec/bptc"
	"github.com/USA-RedDragon/dmrgo/dmr/layer2/elements"
)

// gfMul multiplies in GF(2^8) modulo x^8 + x^4 + x^3 + x^2 + 1 (TS 102 361-1 B.14).
func gfMul(a, b byte) byte {
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		carry := a & 0x80
		a <<= 1
		if carry != 0 {
			a ^= 0x1D
		}
		b >>= 1
	}
	return p
}

// rs129Parity returns the RS(12,9) parity bytes p2, p1, p0 of a 9-byte LC,
// before the Table B.21 mask. The generator is
// g(x) = x^3 + 0x0E x^2 + 0x38 x + 0x40 (TS 102 361-1 B.3.6).
//
// dmrgo v0.0.1 reed_solomon.Encode uses a different generator, so its
// parity is rejected by MMDVMHost and MOTOTRBO radios.
func rs129Parity(lc []byte) [3]byte {
	var p [3]byte
	for _, m := range lc[:9] {
		fb := m ^ p[0]
		p[0] = p[1] ^ gfMul(fb, 0x0E)
		p[1] = p[2] ^ gfMul(fb, 0x38)
		p[2] = gfMul(fb, 0x40)
	}
	return p
}

// fullLCMask returns the Table B.21 RS(12,9) mask byte for a Full LC data type.
func fullLCMask(dataType elements.DataType) byte {
	if dataType == elements.DataTypeTerminatorWithLC {
		return 0x99
	}
	return 0x96
}

// setFullLCParity writes the masked RS(12,9) parity of lc[0:9] into lc[9:12].
// IPSC carries the parity masked, the same as on air.
func setFullLCParity(lc *[12]byte, dataType elements.DataType) {
	p := rs129Parity(lc[:9])
	mask := fullLCMask(dataType)
	for i := range p {
		lc[9+i] = p[i] ^ mask
	}
}

// fullLCParityOK reports whether lc[9:12] holds the masked RS(12,9) parity of lc[0:9].
func fullLCParityOK(lc [12]byte, dataType elements.DataType) bool {
	want := lc
	setFullLCParity(&want, dataType)
	return want == lc
}

// fullLCMatchesPacket reports whether a voice Full LC names the same call as pkt.
func fullLCMatchesPacket(lc [12]byte, pkt models.Packet) bool {
	flco := byte(enums.FLCOUnitToUnitVoiceChannelUser)
	if pkt.GroupCall {
		flco = byte(enums.FLCOGroupVoiceChannelUser)
	}
	dst := uint(lc[3])<<16 | uint(lc[4])<<8 | uint(lc[5])
	src := uint(lc[6])<<16 | uint(lc[7])<<8 | uint(lc[8])
	return lc[0]&0x3F == flco && dst == pkt.Dst && src == pkt.Src
}

// decodeBPTCPayload returns the 96 info bits of a BPTC(196,96) coded data
// burst as 12 bytes. ok is false when the FEC cannot correct the burst.
func decodeBPTCPayload(dmrData [33]byte) (payload [12]byte, ok bool) {
	var bits [196]byte
	for i := 0; i < 98; i++ {
		bits[i] = (dmrData[i/8] >> (7 - i%8)) & 1
		j := 166 + i
		bits[98+i] = (dmrData[j/8] >> (7 - j%8)) & 1
	}
	var codec bptc.BPTC19696
	info, _, uncorrectable := codec.DeinterleaveDataBits(bits)
	for i, b := range info {
		payload[i/8] |= b << (7 - i%8)
	}
	return payload, !uncorrectable
}
