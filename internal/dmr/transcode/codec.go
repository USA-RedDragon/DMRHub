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

// Package transcode converts DMR voice calls between Codec2 and AMBE+2.
// AMBE+2 runs on an AMBE server that speaks the AMBEserver protocol over
// UDP; Codec2 runs in the hub (dmrgo vocoder/codec2,
// on the WebAssembly build of libcodec2 when cgo is off).
package transcode

import (
	"math/bits"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
)

// Codec is the vocoder of a call.
type Codec uint8

// The codecs. CodecUnknown is a burst that carries no voice, such as a
// data burst.
const (
	CodecUnknown Codec = iota
	CodecAMBE
	CodecCodec2
)

// Names of the codecs in the database and the API.
const (
	NameAMBE   = "ambe"
	NameCodec2 = "codec2"
)

// String returns the name of the codec, or "" for CodecUnknown.
func (c Codec) String() string {
	switch c {
	case CodecAMBE:
		return NameAMBE
	case CodecCodec2:
		return NameCodec2
	case CodecUnknown:
		return ""
	}
	return ""
}

// ParseCodec returns the codec named s, or CodecUnknown.
func ParseCodec(s string) Codec {
	switch s {
	case NameAMBE:
		return CodecAMBE
	case NameCodec2:
		return CodecCodec2
	}
	return CodecUnknown
}

// Other returns the codec that a call in c is converted to.
func (c Codec) Other() Codec {
	switch c {
	case CodecAMBE:
		return CodecCodec2
	case CodecCodec2:
		return CodecAMBE
	case CodecUnknown:
		return CodecUnknown
	}
	return CodecUnknown
}

// Codec2FID is the Feature set ID that marks a Codec2 call in its LC.
const Codec2FID = codec2.DefaultFID

// FIDOf returns the Feature set ID of a call in c.
func FIDOf(c Codec) enums.FID {
	if c == CodecCodec2 {
		return Codec2FID
	}
	return enums.FIDStandardized
}

// DataTypeOf returns the data type of a data burst p.
func DataTypeOf(p *models.Packet) (enums.DataType, bool) {
	if p.FrameType != dmrconst.FrameDataSync {
		return 0, false
	}
	return enums.DataType(p.DTypeOrVSeq & 0xF), true //nolint:gosec
}

// IsVoice reports whether p is a voice burst A to F.
func IsVoice(p *models.Packet) bool {
	return p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync
}

// IsHeader reports whether p is a Voice LC Header.
func IsHeader(p *models.Packet) bool {
	dt, ok := DataTypeOf(p)
	return ok && dt == enums.DataTypeVoiceLCHeader
}

// LCValid reports whether p is a Voice LC Header or Terminator with LC
// whose LC passes its Reed-Solomon check after the BPTC FEC.
func LCValid(p *models.Packet) bool {
	dt, ok := DataTypeOf(p)
	if !ok || (dt != enums.DataTypeVoiceLCHeader && dt != enums.DataTypeTerminatorWithLC) {
		return false
	}
	_, ok = layer2.RawBurst(p.DMRData).MMDVMHostPayload(dt)
	return ok
}

// HeaderLC returns the LC of p when p is a Voice LC Header whose LC passes
// its Reed-Solomon check after the BPTC FEC.
func HeaderLC(p *models.Packet) (pdu.LCBytes, bool) {
	dt, ok := DataTypeOf(p)
	if !ok || dt != enums.DataTypeVoiceLCHeader {
		return pdu.LCBytes{}, false
	}
	payload, ok := layer2.RawBurst(p.DMRData).MMDVMHostPayload(dt)
	if !ok {
		return pdu.LCBytes{}, false
	}
	return pdu.LCBytes(payload[:9]), true
}

// HeaderCodec returns the codec of the call that the Voice LC Header p
// starts: Codec2 for the FID 0xC2, AMBE for any other. ok is false when p
// is not a Voice LC Header with a valid LC. The codec never comes from the
// voice bits.
func HeaderCodec(p *models.Packet) (Codec, pdu.LCBytes, bool) {
	lc, ok := HeaderLC(p)
	if !ok {
		return CodecUnknown, lc, false
	}
	return codecOfLC(lc), lc, true
}

func codecOfLC(lc pdu.LCBytes) Codec {
	if lc.FID() == Codec2FID {
		return CodecCodec2
	}
	return CodecAMBE
}

// Encrypted reports whether the voice LC lc asks for privacy, so that its
// voice cannot be converted.
func Encrypted(lc pdu.LCBytes) bool {
	so, ok := lc.ServiceOptions()
	return ok && so.IsPrivacy
}

const (
	modeDecide = 5
	modeLimit  = 8
	modeQuiet  = 5
)

// ModeDetector learns the Codec2 mode of a call from the padding of its
// frames: Mode2400 leaves the bits after 6 octets zero, Mode3200 those
// after 8. Frames with few set bits, such as silence, do not count.
type ModeDetector struct {
	score int8
}

// Add counts the frame slot.
func (d *ModeDetector) Add(slot [9]byte) {
	head := bits.OnesCount32(uint32(slot[0])<<16|uint32(slot[1])<<8|uint32(slot[2])) +
		bits.OnesCount32(uint32(slot[3])<<16|uint32(slot[4])<<8|uint32(slot[5]))
	if head <= modeQuiet {
		return
	}
	switch tail := bits.OnesCount16(uint16(slot[6])<<8 | uint16(slot[7])); {
	case tail <= 1:
		d.score = max(d.score-1, -modeLimit)
	case tail >= 3:
		d.score = min(d.score+1, modeLimit)
	}
}

// Mode returns the mode of the frames so far, or codec2.ModeAuto while
// they do not decide it.
func (d *ModeDetector) Mode() codec2.Mode {
	switch {
	case d.score >= modeDecide:
		return codec2.Mode3200
	case d.score <= -modeDecide:
		return codec2.Mode2400
	default:
		return codec2.ModeAuto
	}
}
