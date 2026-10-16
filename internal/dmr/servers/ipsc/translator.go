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
	headersSent  int  // number of voice headers sent (3 required)
	burstIndex   int  // 0-5 → A-F
	firstPacket  bool // true for the very first packet
	flcCached    bool // whether flcBytes is valid
	flcBytes     [12]byte
	lastActivity time.Time // tracks when this stream was last active
}

// IPSC burst data type constants (byte 30 of IPSC voice packet)
const (
	ipscBurstVoiceHead byte = 0x01
	ipscBurstVoiceTerm byte = 0x02
	ipscBurstCSBK      byte = 0x03
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
			enums.DataTypeDataHeader, enums.DataTypeRate12,
			enums.DataTypeMBCHeader, enums.DataTypeMBCContinuation:
			// Data packet — build IPSC data packet
			data := t.buildIPSCDataPacket(pkt, ss, enums.DataType(dtypeOrVSeq))
			if data == nil {
				return nil
			}
			results = [][]byte{data}
			ss.firstPacket = false
		case enums.DataTypeRate34, enums.DataTypeRate1:
			// TODO: Rate 3/4 and Rate 1 blocks are dropped because no verified IPSC
			// layout exists for them. node-dmr-lib reads rate 3/4 as burst type 8 with
			// 18 bytes and the DBSN+CRC-9 moved to the end, unverified. Capture a
			// confirmed rate 3/4 and a rate 1 transfer through a MOTOTRBO repeater on
			// IPSC, derive the layout, and add both directions with tests.
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
	flcBytes := t.ipscFullLC(pkt, ss, enums.DataTypeVoiceLCHeader)
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
	flcBytes := t.ipscFullLC(pkt, ss, enums.DataTypeTerminatorWithLC)
	copy(buf[38:50], flcBytes[:12])

	ss.ipscSeq++
	return buf
}

// buildIPSCDataPacket builds a 54-byte IPSC data packet for CSBK, Data Header, etc.
// The structure is identical to voice header/terminator but with data packet types (0x83/0x84).
// It returns nil when the BPTC(196,96) payload of the burst cannot be corrected.
func (t *IPSCTranslator) buildIPSCDataPacket(pkt models.Packet, ss *streamState, dataType enums.DataType) []byte {
	payload, res := layer2.RawBurst(pkt.DMRData).BPTCPayload()
	if res.Uncorrectable {
		slog.Debug("IPSCTranslator: uncorrectable data burst", "dtype", dataType)
		return nil
	}

	buf := make([]byte, 54)

	t.buildIPSCHeader(buf, pkt, ss, false, true)

	// RTP header: no marker, payload type 0x5D
	t.buildRTPHeader(buf, ss, ss.firstPacket, 0x5D)

	// RTP Payload — data burst
	buf[30] = byte(dataType) // Burst type = DMR data type (e.g. 0x03 for CSBK)
	buf[31] = 0xC0           // RSSI threshold / parity
	binary.BigEndian.PutUint16(buf[32:34], 0x000A)
	buf[34] = 0x80 // RSSI status
	if pkt.Slot {
		buf[35] = ipscBurstSlot2 // Slot type/sync
	} else {
		buf[35] = ipscBurstSlot1
	}
	binary.BigEndian.PutUint16(buf[36:38], 0x0060) // Data size (96 bits = 12 bytes)

	// Bytes 38-49: BPTC(196,96) payload of the DMR burst
	copy(buf[38:50], payload[:])

	// Bytes 50-53: trailing (zeros)
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
	ambeData := vocoder.PackAMBEVoice(frames)
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

// extractFullLCBytes builds the 9 Full Link Control data bytes from the
// packet fields. The 3 parity bytes are left zero; see setFullLCParity.
func extractFullLCBytes(pkt models.Packet) [12]byte {
	if pkt.Dst > dmrconst.MaxDMRAddress || pkt.Src > dmrconst.MaxDMRAddress {
		slog.Error("Full LC address out of range")
		return [12]byte{}
	}
	lc, err := pdu.NewVoiceLCBytes(pkt.GroupCall, uint32(pkt.Dst), uint32(pkt.Src), layer3.MotorolaServiceOptions())
	if err != nil {
		slog.Error("Failed to encode Full LC", "error", err)
		return [12]byte{}
	}

	var res [12]byte
	copy(res[:9], lc[:])
	return res
}

// ipscFullLC returns the 12-byte Full LC codeword (masked parity) for an IPSC
// voice header or terminator. It passes the received LC through when it is
// valid and names the same call as pkt, and builds one from pkt otherwise.
func (t *IPSCTranslator) ipscFullLC(pkt models.Packet, ss *streamState, dataType enums.DataType) [12]byte {
	if lc, res := layer2.RawBurst(pkt.DMRData).BPTCPayload(); !res.Uncorrectable && fullLCParityOK(lc, dataType) && fullLCMatchesPacket(lc, pkt) {
		return lc
	}
	lc := t.getOrCacheFLC(pkt, ss)
	setFullLCParity(&lc, dataType)
	return lc
}

// getOrCacheFLC returns cached Full LC bytes for the stream, computing them
// on first call. Within a single call the src/dst/groupCall never change,
// so we can cache the expensive Encode() call.
func (t *IPSCTranslator) getOrCacheFLC(pkt models.Packet, ss *streamState) [12]byte {
	if ss.flcCached {
		return ss.flcBytes
	}
	ss.flcBytes = extractFullLCBytes(pkt)
	ss.flcCached = true
	return ss.flcBytes
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
// It constructs the 33-byte DMR burst from the IPSC payload data using BPTC encoding.
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

	// Extract payload bytes from IPSC packet (bytes 38-49 = 12 bytes)
	var lcBytes [12]byte
	needParity := false
	if len(ipscData) >= 50 {
		copy(lcBytes[:], ipscData[38:50])
	} else {
		needParity = true
		// Construct from packet fields
		lcBytes[1] = 0x00
		lcBytes[2] = 0x20
		lcBytes[3] = byte(dst >> 16)
		lcBytes[4] = byte(dst >> 8)
		lcBytes[5] = byte(dst)
		lcBytes[6] = byte(src >> 16)
		lcBytes[7] = byte(src >> 8)
		lcBytes[8] = byte(src)
	}

	// For voice LC headers and terminators, override the FLCO byte to match
	// the group/private flag from the IPSC packet type.
	// The parity covers this byte, so recompute it after a change.
	if dataType == enums.DataTypeVoiceLCHeader || dataType == enums.DataTypeTerminatorWithLC {
		flco := byte(enums.FLCOUnitToUnitVoiceChannelUser)
		if groupCall {
			flco = byte(enums.FLCOGroupVoiceChannelUser)
		}
		if lcBytes[0] != flco {
			lcBytes[0] = flco
			needParity = true
		}
		if needParity {
			setFullLCParity(&lcBytes, dataType)
		}
	}
	// For CSBK/data types, preserve the payload bytes as-is from the radio

	// Build the 33-byte DMR data burst
	burst, err := layer2.BuildDataBurst(lcBytes, dataType, 0, layer2.SyncSourceBS)
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

	var dmrData layer2.RawBurst
	dmrData.SetAMBEFrames(frames)
	if burstIdx == 0 {
		if err := dmrData.SetSync(enums.SyncMSSourcedVoice); err != nil {
			return models.Packet{}, false
		}
	} else {
		emb, frag := embeddedSignalling(burstIdx, ipscData)
		dmrData.SetEMB(emb)
		dmrData.SetEmbeddedData(frag)
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

// embeddedSignalling returns the EMB and the embedded signalling field of
// voice burst B-F (burstIdx 1-5) from the IPSC packet's trailing data.
func embeddedSignalling(burstIdx int, ipscData []byte) (emb pdu.EmbeddedSignalling, frag [4]byte) {
	// Set LCSS based on burst position. The LC fills bursts B-E; burst F
	// carries non-LC signalling, which uses Single fragment (TS 102 361-1 7.1.3).
	switch burstIdx {
	case 1: // Burst B — first fragment
		emb.LCSS = enums.LCSSFirstFragment
	case 4: // Burst E — last fragment
		emb.LCSS = enums.LCSSLastFragment
	case 5: // Burst F — single fragment
		emb.LCSS = enums.LCSSSingleFragment
	default: // Bursts C, D — continuation
		emb.LCSS = enums.LCSSContinuationFragment
	}

	// Bursts B, C, D, F carry 5 bytes at [52:57] and burst E 7 bytes at [52:59].
	if len(ipscData) == 57 || len(ipscData) == 66 {
		copy(frag[:], ipscData[52:56])
	}
	return emb, frag
}
