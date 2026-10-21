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
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

// IPSCTranslator converts MMDVM DMRD packets into IPSC user packets.
// It maintains per-stream state (RTP sequence, timestamp, call control)
// and uses the dmrgo library to FEC-decode AMBE voice data from the
// 33-byte DMR burst into the 19-byte IPSC AMBE payload.
//
// It also converts IPSC user packets back into MMDVM DMRD packets for the
// reverse direction.
type IPSCTranslator struct {
	mu             sync.Mutex
	peerID         uint32
	repeaterID     uint32
	streams        map[uint32]*streamState
	reverseStreams map[uint32]*reverseStreamState
	burst          layer2.Burst // reusable burst to reduce allocations

	nextCallControl uint32
	nextStreamID    uint32
}

// streamState tracks RTP sequencing and call framing for one voice stream.
type streamState struct {
	callControl  uint32 // random per-call
	rtpSeq       uint16
	rtpTimestamp uint32
	ipscSeq      uint8
	headersSent  int       // number of voice headers sent (3 required)
	burstIndex   int       // 0-5 → A-F
	firstPacket  bool      // true for the very first packet
	lastActivity time.Time // tracks when this stream was last active
}

// IPSC burst data type constants (byte 30 of IPSC voice packet)
const (
	ipscBurstVoiceHead byte = 0x01
	ipscBurstVoiceTerm byte = 0x02
	ipscBurstCSBK      byte = 0x03
	ipscBurstRate34    byte = 0x08 // rate 3/4 block, see ipscRate34
	ipscBurstRaw       byte = 0x1F // undecodable block: the 196 info bits
	ipscBurstSlot1     byte = 0x0A
	ipscBurstSlot2     byte = 0x8A
)

// RTP timestamp increment per burst (~60ms spacing in 16.16 format)
const rtpTimestampIncrement = 480

func NewIPSCTranslator(peerID uint32) *IPSCTranslator {
	return &IPSCTranslator{
		streams:        make(map[uint32]*streamState),
		reverseStreams: make(map[uint32]*reverseStreamState),
		peerID:         peerID,
		repeaterID:     peerID,
	}
}

// TranslateToIPSC converts an MMDVM DMRD Packet into one or more IPSC
// user packets ready to send to IPSC peers. It returns nil if the packet
// cannot be translated (e.g. non-voice data we don't handle yet).
func (t *IPSCTranslator) TranslateToIPSC(pkt models.Packet) [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()

	streamID := pkt.StreamID
	if streamID > math.MaxUint32 {
		return nil
	}

	// Get or create stream state
	ss, ok := t.streams[uint32(streamID)]
	if !ok {
		t.nextCallControl++
		if t.nextCallControl == 0 {
			t.nextCallControl = 1
		}
		ss = &streamState{
			callControl:  t.nextCallControl,
			firstPacket:  true,
			lastActivity: time.Now(),
		}
		t.streams[uint32(streamID)] = ss
	}
	ss.lastActivity = time.Now()

	frameType := pkt.FrameType
	dtypeOrVSeq := pkt.DTypeOrVSeq

	var results [][]byte

	switch frameType {
	case dmrconst.FrameDataSync:
		if dtypeOrVSeq > 255 {
			slog.Debug("IPSCTranslator: invalid dtype", "dtype", dtypeOrVSeq)
			return nil
		}
		// Voice LC Header, Terminator, or Data
		switch enums.DataType(dtypeOrVSeq) {
		case enums.DataTypeVoiceLCHeader:
			// Send voice header (IPSC sends 3 copies)
			results = make([][]byte, 0, 3)
			for i := 0; i < 3; i++ {
				data := t.buildVoiceHeader(pkt, ss, i == 0 && ss.firstPacket)
				results = append(results, data)
			}
			ss.headersSent = 3
			ss.firstPacket = false
			ss.burstIndex = 0
		case enums.DataTypeTerminatorWithLC:
			data := t.buildVoiceTerminator(pkt, ss)
			results = [][]byte{data}
			// Clean up stream state
			delete(t.streams, uint32(streamID))
		case enums.DataTypeCSBK, enums.DataTypePIHeader,
			enums.DataTypeDataHeader, enums.DataTypeRate12, enums.DataTypeRate34,
			enums.DataTypeMBCHeader, enums.DataTypeMBCContinuation:
			// Data packet — build IPSC data packet
			data := t.buildIPSCDataPacket(pkt, ss, enums.DataType(dtypeOrVSeq))
			if data == nil {
				return nil
			}
			results = [][]byte{data}
			ss.firstPacket = false
		case enums.DataTypeRate1:
			// Rate 1 blocks are dropped: IPSC has no known rate 1 layout. An
			// XPR 8400 sent clean rate 1 blocks to IPSC as burst type 0x06 with
			// a BPTC(196,96) decode of the uncoded bits, or as an undecodable
			// block (0x1F), so its firmware does not carry rate 1 either.
			slog.Debug("IPSCTranslator: unsupported data dtype", "dtype", dtypeOrVSeq)
			return nil
		case enums.DataTypeIdle, enums.DataTypeUnifiedSingleBlock, enums.DataTypeReserved:
			return nil
		default:
			slog.Debug("IPSCTranslator: unhandled data sync dtype", "dtype", dtypeOrVSeq)
			return nil
		}

	case dmrconst.FrameVoice, dmrconst.FrameVoiceSync:
		// Voice burst — decode DMR data and extract AMBE
		data := t.buildVoiceBurst(pkt, ss)
		if data != nil {
			results = [][]byte{data}
		}
		// Advance burst index (A=0 through F=5, then wrap)
		ss.burstIndex = (ss.burstIndex + 1) % 6

	default:
		slog.Debug("IPSCTranslator: unknown frame type", "frameType", frameType)
		return nil
	}

	return results
}

// CleanupStream removes state for a given stream (e.g. on timeout).
func (t *IPSCTranslator) CleanupStream(streamID uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.streams, streamID)
}

// CleanupReverseStream removes reverse stream state for a given call control ID.
func (t *IPSCTranslator) CleanupReverseStream(callControl uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.reverseStreams, callControl)
}

// CleanupStaleStreams removes any forward or reverse stream entries that
// have not been active within the given maxAge duration. This prevents
// unbounded growth of the stream maps when UDP terminator packets are lost.
func (t *IPSCTranslator) CleanupStaleStreams(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for id, ss := range t.streams {
		if now.Sub(ss.lastActivity) > maxAge {
			delete(t.streams, id)
			cleaned++
		}
	}
	for id, rss := range t.reverseStreams {
		if now.Sub(rss.lastActivity) > maxAge {
			delete(t.reverseStreams, id)
			cleaned++
		}
	}

	if cleaned > 0 {
		slog.Debug("IPSCTranslator: cleaned stale streams", "count", cleaned)
	}

	return cleaned
}

// StreamCount returns the number of active forward and reverse streams.
func (t *IPSCTranslator) StreamCount() (forward, reverse int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.streams), len(t.reverseStreams)
}

// buildIPSCHeader writes the common 18-byte IPSC header (bytes 0-17).
func (t *IPSCTranslator) buildIPSCHeader(buf []byte, pkt models.Packet, ss *streamState, isEnd bool, isData bool) {
	// Byte 0: Packet type
	if isData {
		if pkt.GroupCall {
			buf[0] = byte(0x83) // GROUP_DATA
		} else {
			buf[0] = byte(0x84) // PVT_DATA
		}
	} else {
		if pkt.GroupCall {
			buf[0] = byte(0x80) // GROUP_VOICE
		} else {
			buf[0] = byte(0x81) // PVT_VOICE
		}
	}

	// Bytes 1-4: Peer ID
	binary.BigEndian.PutUint32(buf[1:5], t.peerID)

	// Byte 5: IPSC sequence number
	buf[5] = ss.ipscSeq

	// Bytes 6-8: Source subscriber (24-bit)
	buf[6] = byte(pkt.Src >> 16)
	buf[7] = byte(pkt.Src >> 8)
	buf[8] = byte(pkt.Src)

	// Bytes 9-11: Destination (24-bit)
	buf[9] = byte(pkt.Dst >> 16)
	buf[10] = byte(pkt.Dst >> 8)
	buf[11] = byte(pkt.Dst)

	// Byte 12: Call type (0x02 = group call)
	if pkt.GroupCall {
		buf[12] = 0x02
	} else {
		buf[12] = 0x01
	}

	// Bytes 13-16: Call control (random per-call)
	binary.BigEndian.PutUint32(buf[13:17], ss.callControl)

	// Byte 17: Call info (timeslot + end flag)
	callInfo := byte(0x00)
	if pkt.Slot { // true = TS2
		callInfo |= 0x20
	}
	if isEnd {
		callInfo |= 0x40
	}
	buf[17] = callInfo
}

// buildRTPHeader writes the 12-byte RTP header at buf[18:30].
func (t *IPSCTranslator) buildRTPHeader(buf []byte, ss *streamState, marker bool, payloadType byte) {
	// Byte 18: RTP version 2, no padding, no extension, 0 CSRCs
	buf[18] = 0x80

	// Byte 19: Marker + payload type
	pt := payloadType
	if marker {
		pt |= 0x80
	}
	buf[19] = pt

	// Bytes 20-21: RTP sequence number
	binary.BigEndian.PutUint16(buf[20:22], ss.rtpSeq)
	ss.rtpSeq++

	// Bytes 22-25: RTP timestamp
	binary.BigEndian.PutUint32(buf[22:26], ss.rtpTimestamp)
	ss.rtpTimestamp += rtpTimestampIncrement

	// Bytes 26-29: RTP SSRC (0)
	binary.BigEndian.PutUint32(buf[26:30], 0)
}

// buildVoiceHeader builds a 54-byte IPSC voice header packet.
// Voice headers embed the Full LC (link control) data.
func (t *IPSCTranslator) buildVoiceHeader(pkt models.Packet, ss *streamState, isFirst bool) []byte {
	buf := make([]byte, 54)

	t.buildIPSCHeader(buf, pkt, ss, false, false)

	// RTP header: marker on first header, payload type 0x5D
	t.buildRTPHeader(buf, ss, isFirst, 0x5D)

	// RTP Payload — voice header
	burstType := ipscBurstSlot2
	if !pkt.Slot {
		burstType = ipscBurstSlot1
	}
	_ = burstType

	buf[30] = ipscBurstVoiceHead                   // Burst type
	buf[31] = 0x80                                 // RSSI threshold / parity
	binary.BigEndian.PutUint16(buf[32:34], 0x000A) // Length to follow (10 words = 20 bytes)
	buf[34] = 0x80                                 // RSSI status
	if pkt.Slot {
		buf[35] = ipscBurstSlot2 // Slot type/sync
	} else {
		buf[35] = ipscBurstSlot1
	}
	binary.BigEndian.PutUint16(buf[36:38], 0x0060) // Data size (96 bits = 12 bytes)

	// Bytes 38-49: Full LC data (12 bytes)
	// The header burst carries a Voice LC Header
	// which contains FLCO, FID, ServiceOpt, Dst, Src, RS(12,9) parity
	flcBytes := ipscFullLC(pkt, enums.DataTypeVoiceLCHeader)
	copy(buf[38:50], flcBytes[:12])

	// Bytes 50-53: unknown trailing (zeros)
	return buf
}

// buildVoiceTerminator builds a 54-byte IPSC voice terminator packet.
func (t *IPSCTranslator) buildVoiceTerminator(pkt models.Packet, ss *streamState) []byte {
	buf := make([]byte, 54)

	t.buildIPSCHeader(buf, pkt, ss, true, false)

	// RTP header: no marker, payload type 0x5E for terminator
	t.buildRTPHeader(buf, ss, false, 0x5E)

	// RTP Payload — voice terminator (same structure as header)
	buf[30] = ipscBurstVoiceTerm
	buf[31] = 0x80
	binary.BigEndian.PutUint16(buf[32:34], 0x000A)
	buf[34] = 0x80
	if pkt.Slot {
		buf[35] = ipscBurstSlot2
	} else {
		buf[35] = ipscBurstSlot1
	}
	binary.BigEndian.PutUint16(buf[36:38], 0x0060)

	// Full LC data
	flcBytes := ipscFullLC(pkt, enums.DataTypeTerminatorWithLC)
	copy(buf[38:50], flcBytes[:12])

	ss.ipscSeq++
	return buf
}

// buildIPSCDataPacket builds an IPSC data packet for CSBK, Data Header, etc.
// The structure is identical to voice header/terminator but with data packet types (0x83/0x84).
// A BPTC(196,96) burst carries its 12 decoded octets and a rate 3/4 burst
// the 18 octets of ipscRate34. A rate 3/4 burst that does not decode goes
// out as ipscBurstRaw with its 196 info bits, as a MOTOTRBO repeater sends
// it. It returns nil when the BPTC(196,96) payload cannot be corrected.
func (t *IPSCTranslator) buildIPSCDataPacket(pkt models.Packet, ss *streamState, dataType enums.DataType) []byte {
	raw := layer2.RawBurst(pkt.DMRData)
	if dataType == enums.DataTypeRate34 {
		if p, res := raw.Rate34Payload(); !res.Uncorrectable {
			rot := ipscRate34(p)
			return t.writeIPSCDataPacket(pkt, ss, ipscBurstRate34, 144, rot[:])
		}
		info := infoBits(raw)
		return t.writeIPSCDataPacket(pkt, ss, ipscBurstRaw, 196, info[:])
	}

	payload, res := raw.BPTCPayload()
	if res.Uncorrectable {
		slog.Debug("IPSCTranslator: uncorrectable data burst", "dtype", dataType)
		return nil
	}
	return t.writeIPSCDataPacket(pkt, ss, byte(dataType), 96, payload[:])
}

// ipscRate34 returns the IPSC payload of a rate 3/4 block: its 18 decoded
// octets rotated left by 2, so octets 0-1 (the DBSN and CRC-9 of a
// confirmed block) come last. This is the layout an XPR 8400 sends.
func ipscRate34(p [18]byte) (o [18]byte) {
	copy(o[:16], p[2:])
	copy(o[16:], p[:2])
	return o
}

// dmrRate34 is the reverse of ipscRate34.
func dmrRate34(o [18]byte) (p [18]byte) {
	copy(p[:2], o[16:])
	copy(p[2:], o[:16])
	return p
}

// infoBits returns the 196 info bits of a data burst (burst bits 0-97 and
// 166-263) packed most significant bit first, before any FEC decode.
func infoBits(b layer2.RawBurst) (info [25]byte) {
	for i := 0; i < 196; i++ {
		j := i
		if i >= 98 {
			j = i + 68
		}
		info[i/8] |= ((b[j/8] >> (7 - j%8)) & 1) << (7 - i%8)
	}
	return info
}

// writeIPSCDataPacket writes an IPSC data packet of burst type burstType
// that carries the bits-bit payload, padded to an even octet count.
func (t *IPSCTranslator) writeIPSCDataPacket(pkt models.Packet, ss *streamState, burstType byte, bits uint16, payload []byte) []byte {
	n := (len(payload) + 1) &^ 1
	buf := make([]byte, 38+n+4)

	t.buildIPSCHeader(buf, pkt, ss, false, true)

	// RTP header: no marker, payload type 0x5D
	t.buildRTPHeader(buf, ss, ss.firstPacket, 0x5D)

	// RTP Payload — data burst
	buf[30] = burstType                                             // Burst type = DMR data type (e.g. 0x03 for CSBK)
	buf[31] = 0xC0                                                  // RSSI threshold / parity
	binary.BigEndian.PutUint16(buf[32:34], uint16((len(buf)-34)/2)) // Length to follow in words
	buf[34] = 0x80                                                  // RSSI status
	if pkt.Slot {
		buf[35] = ipscBurstSlot2 // Slot type/sync
	} else {
		buf[35] = ipscBurstSlot1
	}
	binary.BigEndian.PutUint16(buf[36:38], bits) // Data size in bits

	copy(buf[38:], payload)

	// Last 4 bytes: trailing (zeros)
	ss.ipscSeq++
	return buf
}

// buildVoiceBurst builds an IPSC voice burst packet.
// Burst A = 52 bytes, Bursts B-D,F = 57 bytes, Burst E = 66 bytes.
func (t *IPSCTranslator) buildVoiceBurst(pkt models.Packet, ss *streamState) []byte {
	raw := layer2.RawBurst(pkt.DMRData)
	if err := t.burst.DecodeFromBytes(raw); err != nil || t.burst.Kind == layer2.KindData {
		slog.Debug("IPSCTranslator: skipping data burst in voice stream")
		return nil
	}

	// Extract the 19-byte FEC-decoded AMBE payload from the 3 vocoder frames
	frames, _ := raw.AMBEFrames()
	ambeData, err := vocoder.PackAMBEVoice(frames)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot pack AMBE frames", "error", err)
		return nil
	}
	embData := raw.EmbeddedData()
	hasEmbedded := t.burst.HasEmbeddedSignalling()

	// Determine slot type byte
	slotBurst := ipscBurstSlot2
	if !pkt.Slot {
		slotBurst = ipscBurstSlot1
	}

	burstIdx := ss.burstIndex % 6

	var buf []byte
	switch burstIdx {
	case 0: // Burst A — sync burst, 52 bytes
		buf = make([]byte, 52)
		t.buildIPSCHeader(buf, pkt, ss, false, false)
		t.buildRTPHeader(buf, ss, false, 0x5D)

		buf[30] = slotBurst
		buf[31] = 0x14 // Length: 20 bytes follow
		buf[32] = 0x40 // Unknown field
		copy(buf[33:52], ambeData[:])

	case 4: // Burst E — extended with embedded LC, 66 bytes
		buf = make([]byte, 66)
		t.buildIPSCHeader(buf, pkt, ss, false, false)
		t.buildRTPHeader(buf, ss, false, 0x5D)

		buf[30] = slotBurst
		buf[31] = 0x22 // Length: 34 bytes follow
		buf[32] = 0x16 // Unknown field
		copy(buf[33:52], ambeData[:])

		// Bytes 52-58: Embedded LC data (7 bytes)
		// Extract from embedded signalling if available
		if hasEmbedded {
			copy(buf[52:56], embData[:])
		}

		// Bytes 56-58 or 59-61: Destination repeated
		buf[59] = byte(pkt.Dst >> 16)
		buf[60] = byte(pkt.Dst >> 8)
		buf[61] = byte(pkt.Dst)
		// Bytes 62-64: Source repeated
		buf[62] = byte(pkt.Src >> 16)
		buf[63] = byte(pkt.Src >> 8)
		buf[64] = byte(pkt.Src)
		buf[65] = 0x14 // Unknown trailer

	default: // Bursts B, C, D, F — 57 bytes with embedded signalling
		buf = make([]byte, 57)
		t.buildIPSCHeader(buf, pkt, ss, false, false)
		t.buildRTPHeader(buf, ss, false, 0x5D)

		buf[30] = slotBurst
		buf[31] = 0x19 // Length: 25 bytes follow
		buf[32] = 0x06 // Unknown field
		copy(buf[33:52], ambeData[:])

		// Bytes 52-56: Embedded signalling data (5 bytes)
		if hasEmbedded {
			copy(buf[52:56], embData[:])
		}
	}

	return buf
}

// extractFullLCBytes builds the 9 Full Link Control octets of a group or
// unit-to-unit voice call from the packet fields.
func extractFullLCBytes(pkt models.Packet) pdu.LCBytes {
	if pkt.Dst > dmrconst.MaxDMRAddress || pkt.Src > dmrconst.MaxDMRAddress {
		slog.Error("Full LC address out of range")
		return pdu.LCBytes{}
	}
	lc, err := pdu.NewVoiceLCBytes(pkt.GroupCall, uint32(pkt.Dst), uint32(pkt.Src), layer3.MotorolaServiceOptions())
	if err != nil {
		slog.Error("Failed to encode Full LC", "error", err)
	}
	return lc
}

// ipscFullLC returns the 12-byte Full LC codeword (masked parity) for an IPSC
// voice header or terminator. It passes the received LC through when it is
// valid and names the same call as pkt, and builds one from pkt otherwise.
func ipscFullLC(pkt models.Packet, dataType enums.DataType) [12]byte {
	if cw, ok := layer2.RawBurst(pkt.DMRData).MMDVMHostPayload(dataType); ok && fullLCMatchesPacket(cw, pkt) {
		return cw
	}
	cw, err := extractFullLCBytes(pkt).Codeword(dataType)
	if err != nil {
		slog.Error("Failed to encode Full LC codeword", "error", err)
	}
	return cw
}

// fullLCMatchesPacket reports whether a voice Full LC names the same call as pkt.
func fullLCMatchesPacket(cw [12]byte, pkt models.Packet) bool {
	lc := pdu.LCBytes(cw[:9])
	group, okGroup := lc.Group()
	target, source, okAddr := lc.Addresses()
	return okGroup && okAddr && group == pkt.GroupCall && uint(target) == pkt.Dst && uint(source) == pkt.Src
}

// reverseStreamState tracks per-call state for IPSC→MMDVM translation.
type reverseStreamState struct {
	streamID     uint32
	seq          uint8
	burstIndex   int       // 0-5 → A-F within a superframe
	started      bool      // whether we've seen a voice header
	lastActivity time.Time // tracks when this stream was last active
}

// TranslateToMMDVM converts raw IPSC user packet data into MMDVM DMRD Packets.
// Returns nil if the packet cannot be translated.
func (t *IPSCTranslator) TranslateToMMDVM(packetType byte, data []byte) []models.Packet {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(data) < 31 {
		slog.Debug("IPSCTranslator: IPSC packet too short", "length", len(data))
		return nil
	}

	// Handle voice (0x80/0x81) and data (0x83/0x84) packet types
	switch packetType {
	case 0x80, 0x81, 0x83, 0x84:
		// OK — supported packet types
	default:
		slog.Debug("IPSCTranslator: ignoring unsupported IPSC packet", "type", packetType)
		return nil
	}

	// Parse the IPSC header
	peerID := binary.BigEndian.Uint32(data[1:5])
	src := uint(data[6])<<16 | uint(data[7])<<8 | uint(data[8])
	dst := uint(data[9])<<16 | uint(data[10])<<8 | uint(data[11])
	groupCall := packetType == 0x80 || packetType == 0x83
	callInfo := data[17]
	slot := (callInfo & 0x20) != 0 // true = TS2
	isEnd := (callInfo & 0x40) != 0

	slog.Debug("IPSCTranslator: TranslateToMMDVM",
		"packetType", fmt.Sprintf("0x%02X", packetType),
		"src", src, "dst", dst, "groupCall", groupCall,
		"slot", slot, "isEnd", isEnd)

	// Use call control bytes as stream identifier
	callControl := binary.BigEndian.Uint32(data[13:17])

	// Get or create reverse stream state
	rss, ok := t.reverseStreams[callControl]
	if !ok {
		t.nextStreamID++
		if t.nextStreamID == 0 {
			t.nextStreamID = 1
		}
		rss = &reverseStreamState{
			streamID:     t.nextStreamID,
			lastActivity: time.Now(),
		}
		t.reverseStreams[callControl] = rss
	}
	rss.lastActivity = time.Now()

	// Determine what kind of IPSC burst this is from byte 30
	burstType := data[30]

	results := make([]models.Packet, 0, 1)

	switch burstType {
	case ipscBurstVoiceHead:
		// Voice LC Header — only process the first one (IPSC sends 3)
		if !rss.started {
			if pkt, ok := t.buildMMDVMDataPacket(src, dst, groupCall, slot, peerID, rss,
				enums.DataTypeVoiceLCHeader, data); ok {
				results = append(results, pkt)
			}
			rss.started = true
			rss.burstIndex = 0
		}
		// Skip duplicate headers

	case ipscBurstVoiceTerm:
		// Voice Terminator
		if pkt, ok := t.buildMMDVMDataPacket(src, dst, groupCall, slot, peerID, rss,
			enums.DataTypeTerminatorWithLC, data); ok {
			results = append(results, pkt)
		}
		// Clean up
		delete(t.reverseStreams, callControl)

	case ipscBurstSlot1, ipscBurstSlot2:
		// Voice burst — extract AMBE, FEC-encode, build DMR burst
		if len(data) < 52 {
			slog.Debug("IPSCTranslator: voice burst too short", "length", len(data))
			return nil
		}

		if pkt, ok := t.buildMMDVMVoiceBurst(src, dst, groupCall, slot, peerID, rss, data); ok {
			results = append(results, pkt)
		}

	case ipscBurstCSBK:
		// CSBK or data burst — same 54-byte structure as voice header
		if pkt, ok := t.buildMMDVMDataPacket(src, dst, groupCall, slot, peerID, rss,
			enums.DataTypeCSBK, data); ok {
			results = append(results, pkt)
		}

	case ipscBurstRaw:
		// An undecodable block does not name its data type, so it cannot be
		// rebuilt as a burst.
		slog.Debug("IPSCTranslator: dropping undecodable IPSC data block")

	default:
		// Treat any other burst type as a generic data packet if it has
		// the same structure as a voice header (54 bytes with LC data).
		// The burst type byte maps directly to the DMR data type.
		if len(data) >= 50 && burstType <= 10 {
			if pkt, ok := t.buildMMDVMDataPacket(src, dst, groupCall, slot, peerID, rss,
				enums.DataType(burstType), data); ok {
				results = append(results, pkt)
			}
		} else {
			slog.Debug("IPSCTranslator: unknown IPSC burst type", "burstType", burstType)
			return nil
		}
	}

	if isEnd && burstType != ipscBurstVoiceTerm {
		// End flag set but not a terminator — clean up anyway
		delete(t.reverseStreams, callControl)
	}

	return results
}

// buildMMDVMDataPacket builds an MMDVM DMRD packet for a voice LC header, terminator,
// or data burst (CSBK, Data Header, etc.).
// It constructs the 33-byte DMR burst from the IPSC payload: BPTC(196,96) for
// the 12-octet payloads and the rate 3/4 code for an ipscRate34 payload.
func (t *IPSCTranslator) buildMMDVMDataPacket(
	src, dst uint, groupCall, slot bool,
	peerID uint32,
	rss *reverseStreamState,
	dataType enums.DataType,
	ipscData []byte,
) (models.Packet, bool) {
	pkt := models.Packet{
		Signature:   "DMRD",
		Seq:         uint(rss.seq),
		Src:         src,
		Dst:         dst,
		Repeater:    uint(peerID),
		Slot:        slot,
		GroupCall:   groupCall,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dataType),
		StreamID:    uint(rss.streamID),
	}
	rss.seq++

	if dataType == enums.DataTypeRate34 {
		if len(ipscData) < 56 {
			slog.Debug("IPSCTranslator: rate 3/4 block too short", "length", len(ipscData))
			return pkt, false
		}
		burst, err := layer2.BuildBurst(&pdu.Rate34Data{Data: dmrRate34([18]byte(ipscData[38:56]))}, 0, layer2.SyncSourceBS)
		if err != nil {
			slog.Debug("IPSCTranslator: cannot build rate 3/4 burst", "error", err)
			return pkt, false
		}
		pkt.DMRData = burst
		return pkt, true
	}

	// Extract payload bytes from IPSC packet (bytes 38-49 = 12 bytes).
	// CSBK and data payloads pass through as they came from the radio.
	var payload [12]byte
	reseal := false
	if len(ipscData) >= 50 {
		copy(payload[:], ipscData[38:50])
	} else {
		lc := extractFullLCBytes(models.Packet{Src: src, Dst: dst, GroupCall: groupCall})
		copy(payload[:9], lc[:])
		reseal = true
	}

	// For voice LC headers and terminators, override the FLCO to match the
	// group/private flag from the IPSC packet type. The parity covers the
	// FLCO, so reseal after a change.
	if dataType.CarriesFullLC() {
		flco := enums.FLCOUnitToUnitVoiceChannelUser
		if groupCall {
			flco = enums.FLCOGroupVoiceChannelUser
		}
		if lc := (*pdu.LCBytes)(payload[:9]); lc.FLCO() != flco {
			if err := lc.SetFLCO(flco); err != nil {
				slog.Debug("IPSCTranslator: cannot set FLCO", "error", err)
				return pkt, false
			}
			reseal = true
		}
		if reseal {
			if err := pdu.Seal(&payload, dataType); err != nil {
				slog.Debug("IPSCTranslator: cannot seal Full LC", "error", err)
				return pkt, false
			}
		}
	}

	// Build the 33-byte DMR data burst
	burst, err := layer2.BuildDataBurst(payload, dataType, 0, layer2.SyncSourceBS)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build data burst", "dtype", dataType, "error", err)
		return pkt, false
	}
	pkt.DMRData = burst

	return pkt, true
}

// buildMMDVMVoiceBurst builds MMDVM DMRD packets from an IPSC voice burst.
// It extracts the 19-byte AMBE payload, FEC-encodes back to DMR format,
// and reconstructs the full 33-byte DMR burst with proper sync/EMB.
func (t *IPSCTranslator) buildMMDVMVoiceBurst(
	src, dst uint, groupCall, slot bool,
	peerID uint32,
	rss *reverseStreamState,
	ipscData []byte,
) (models.Packet, bool) {
	// Extract the 19-byte AMBE data from IPSC packet (bytes 33-51)
	var ambeBytes [19]byte
	copy(ambeBytes[:], ipscData[33:52])

	frames := vocoder.UnpackAMBEVoice(ambeBytes)

	// Determine if this is a sync burst (A) or embedded signalling burst (B-F)
	burstIdx := rss.burstIndex % 6

	pos, _ := enums.VoiceBurstAt(burstIdx)
	dmrData, err := layer2.BuildVoiceBurst(frames, pos, pdu.EmbeddedSignalling{},
		embeddedFragment(ipscData), layer2.SyncSourceMS)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build voice burst", "error", err)
		return models.Packet{}, false
	}

	frameType := dmrconst.FrameVoice
	if burstIdx == 0 {
		frameType = dmrconst.FrameVoiceSync
	}

	pkt := models.Packet{
		Signature:   "DMRD",
		Seq:         uint(rss.seq),
		Src:         src,
		Dst:         dst,
		Repeater:    uint(peerID),
		Slot:        slot,
		GroupCall:   groupCall,
		FrameType:   frameType,
		DTypeOrVSeq: uint(burstIdx), //nolint:gosec // Bounds checked
		StreamID:    uint(rss.streamID),
		DMRData:     dmrData,
	}
	rss.seq++
	rss.burstIndex = (rss.burstIndex + 1) % 6

	return pkt, true
}

// embeddedFragment returns the embedded signalling field of a voice burst
// B-F from the IPSC packet's trailing data. Bursts B, C, D, F carry 5 bytes
// at [52:57] and burst E 7 bytes at [52:59].
func embeddedFragment(ipscData []byte) (frag [4]byte) {
	if len(ipscData) == 57 || len(ipscData) == 66 {
		copy(frag[:], ipscData[52:56])
	}
	return frag
}
