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
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
)

// IPSCTranslator converts MMDVM DMRD packets into IPSC user packets and
// back. It keeps per-stream state (RTP sequence, timestamp, call control)
// and uses dmrgo transport/ipsc for the IPSC packets. In the IPSC to MMDVM
// direction a dmrgo voice.Receiver per call places each voice burst in the
// superframe.
type IPSCTranslator struct {
	mu             sync.Mutex
	peerID         uint32
	repeaterID     uint32
	streams        map[uint32]*streamState
	reverseStreams map[uint32]*reverseStreamState
	burst          *layer2.Burst // reusable burst to reduce allocations
	events         []voice.Event
	epoch          time.Time

	nextCallControl uint32
	nextStreamID    uint32

	rawBlocksAnXPRDrops bool
	onUndeliverable     func(pkt models.Packet, reason string)
}

// sendRawBlocksAnXPRDrops sends to IPSC peers, as undecodable blocks (burst
// type 0x1F) with their 196 information bits and slot type, the bursts
// that an XPR 8400 does not transmit when it gets them that way: idle,
// USBD and reserved data types, and CSBK, data header and data blocks
// whose FEC does not correct. The 2026-10-22 rig test showed that the XPR
// drops each of them without a word, so they are reported undeliverable
// while this is false. Rate 1 blocks always go as raw blocks, since the
// XPR transmits those bit for bit.
//
//nolint:gochecknoglobals
var sendRawBlocksAnXPRDrops = false

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
	lc           [9]byte   // the Full LC of the call, for burst E
	haveLC       bool      // whether lc is set
}

// IPSC burst types (byte 30 of an IPSC user packet).
const (
	ipscBurstVoiceHead = byte(enums.DataTypeVoiceLCHeader)
	ipscBurstVoiceTerm = byte(enums.DataTypeTerminatorWithLC)
	ipscBurstCSBK      = byte(enums.DataTypeCSBK)
	ipscBurstRate34    = byte(enums.DataTypeRate34) // rate 3/4 block
	ipscBurstRaw       = byte(ipscwire.BurstRaw)    // undecodable block: the 196 info bits
	ipscBurstSlot1     = byte(ipscwire.BurstVoice)
	ipscBurstSlot2     = byte(ipscwire.BurstVoice) | 0x80
)

// RTP timestamp increment per burst (~60ms spacing in 16.16 format)
const rtpTimestampIncrement = 480

// RTP payload types of MOTOTRBO: 0x5D on every packet but the last of a call.
const (
	rtpPayloadType    = 0x5D
	rtpPayloadTypeEnd = 0x5E
	rtpVersion2       = 0x80
)

// IPSC priority octets of MOTOTRBO, as an XPR 8400 sends them.
const (
	ipscPriorityVoice = 0x02
	ipscPriorityData  = 0x01
)

func NewIPSCTranslator(peerID uint32) *IPSCTranslator {
	return &IPSCTranslator{
		streams:             make(map[uint32]*streamState),
		reverseStreams:      make(map[uint32]*reverseStreamState),
		peerID:              peerID,
		repeaterID:          peerID,
		burst:               layer2.NewBurst(),
		events:              make([]voice.Event, 0, voice.MaxEvents),
		epoch:               time.Now(),
		rawBlocksAnXPRDrops: sendRawBlocksAnXPRDrops,
	}
}

// The reasons that undeliverable reports. They are stable: last heard
// stores them as "ipsc:<repeater>:<reason>".
const (
	reasonInvalidDataType = "invalid_data_type"
	reasonNoIPSCForm      = "no_ipsc_form"
	reasonFrameType3      = "frame_type_3"
	reasonFEC             = "fec_uncorrectable"
	reasonSlotType        = "slot_type"
)

// undeliverable reports a burst that cannot go to IPSC peers. The caller
// holds t.mu.
func (t *IPSCTranslator) undeliverable(pkt models.Packet, reason string) {
	if t.onUndeliverable != nil {
		t.onUndeliverable(pkt, reason)
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

	dtypeOrVSeq := pkt.DTypeOrVSeq

	var results [][]byte

	switch pkt.FrameType {
	case dmrconst.FrameDataSync:
		if dtypeOrVSeq > 255 {
			t.undeliverable(pkt, reasonInvalidDataType)
			return nil
		}
		// Voice LC Header, Terminator, or Data
		switch dt := enums.DataType(dtypeOrVSeq); dt {
		case enums.DataTypeVoiceLCHeader:
			// Send voice header (IPSC sends 3 copies)
			results = make([][]byte, 0, 3)
			for i := 0; i < 3; i++ {
				if data := t.buildVoiceLC(pkt, ss, dt, i == 0 && ss.firstPacket); data != nil {
					results = append(results, data)
				}
			}
			ss.headersSent = 3
			ss.firstPacket = false
			ss.burstIndex = 0
		case enums.DataTypeTerminatorWithLC:
			if data := t.buildVoiceLC(pkt, ss, dt, false); data != nil {
				results = [][]byte{data}
			}
			ss.ipscSeq++
			// Clean up stream state
			delete(t.streams, uint32(streamID))
		case enums.DataTypeCSBK, enums.DataTypePIHeader,
			enums.DataTypeDataHeader, enums.DataTypeRate12, enums.DataTypeRate34,
			enums.DataTypeMBCHeader, enums.DataTypeMBCContinuation:
			// Data packet — build IPSC data packet
			data := t.buildIPSCDataPacket(pkt, ss, dt)
			if data == nil {
				return nil
			}
			results = [][]byte{data}
			ss.firstPacket = false
		case enums.DataTypeRate1:
			data := t.buildRawBlock(pkt, ss, dt)
			if data == nil {
				return nil
			}
			results = [][]byte{data}
			ss.firstPacket = false
		case enums.DataTypeIdle, enums.DataTypeUnifiedSingleBlock, enums.DataTypeReserved:
			fallthrough
		default:
			if !t.rawBlocksAnXPRDrops {
				t.undeliverable(pkt, reasonNoIPSCForm)
				return nil
			}
			data := t.buildRawBlock(pkt, ss, dt)
			if data == nil {
				return nil
			}
			results = [][]byte{data}
			ss.firstPacket = false
		}

	case dmrconst.FrameVoice, dmrconst.FrameVoiceSync:
		if data := t.buildVoiceBurst(pkt, ss); data != nil {
			results = [][]byte{data}
		}

	default:
		t.undeliverable(pkt, reasonFrameType3)
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

// user returns the IPSC user packet of pkt without its burst, and advances
// the RTP sequence number and timestamp of the stream.
func (t *IPSCTranslator) user(pkt models.Packet, ss *streamState, isData, isEnd, marker bool) ipscwire.User {
	u := ipscwire.User{
		ID:          t.peerID,
		CallSeq:     ss.ipscSeq,
		Source:      uint32(pkt.Src & dmrconst.MaxDMRAddress),
		Destination: uint32(pkt.Dst & dmrconst.MaxDMRAddress),
		CallControl: ss.callControl,
		RTP: ipscwire.RTP{
			Flags:       rtpVersion2,
			Marker:      marker,
			PayloadType: rtpPayloadType,
			Seq:         ss.rtpSeq,
			Timestamp:   ss.rtpTimestamp,
		},
	}
	switch {
	case isData && pkt.GroupCall:
		u.Opcode, u.Priority = ipscwire.OpGroupData, ipscPriorityData
	case isData:
		u.Opcode, u.Priority = ipscwire.OpPrivateData, ipscPriorityData
	case pkt.GroupCall:
		u.Opcode, u.Priority = ipscwire.OpGroupVoice, ipscPriorityVoice
	default:
		u.Opcode, u.Priority = ipscwire.OpPrivateVoice, ipscPriorityVoice
	}
	if pkt.Slot { // true = TS2
		_ = u.Info.SetSlot(enums.Slot2)
	}
	if isEnd {
		u.Info.SetEnd(true)
		u.RTP.PayloadType = rtpPayloadTypeEnd
	}
	ss.rtpSeq++
	ss.rtpTimestamp += rtpTimestampIncrement
	return u
}

// encodeUser returns the octets of u, or nil when it does not encode.
func encodeUser(u *ipscwire.User) []byte {
	data, err := u.AppendBinary(nil)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot encode IPSC packet", "error", err)
		return nil
	}
	return data
}

// buildVoiceLC builds an IPSC voice header or terminator, which carries
// the Full LC of the call.
func (t *IPSCTranslator) buildVoiceLC(pkt models.Packet, ss *streamState, dt enums.DataType, marker bool) []byte {
	u := t.user(pkt, ss, false, dt == enums.DataTypeTerminatorWithLC, marker)
	// The LC comes from ipscFullLC, which checks it, so the packet carries
	// a burst rebuilt from it with the color code of the burst.
	cw := ipscFullLC(pkt, dt)
	raw, err := layer2.BuildDataBurst(cw, dt, layer2.RawBurst(pkt.DMRData).SlotType().ColorCode, layer2.SyncSourceBS)
	if err == nil {
		_, err = u.SetRawBurst(raw, layer2.Hint{Kind: layer2.KindData, DataType: dt, HasDataType: true})
	}
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build voice LC packet", "dtype", dt, "error", err)
		return nil
	}
	ss.lc, ss.haveLC = [9]byte(cw[:9]), true
	return encodeUser(&u)
}

// buildIPSCDataPacket builds an IPSC data packet for CSBK, Data Header, etc.
// A BPTC(196,96) burst carries its 12 decoded octets and a rate 3/4 burst
// its 18. A burst whose FEC does not correct goes out as ipscBurstRaw with
// its 196 info bits, as a MOTOTRBO repeater sends it and SetRawBurst builds
// it, when sendRawBlocksAnXPRDrops is on; otherwise it is reported
// undeliverable and it returns nil.
func (t *IPSCTranslator) buildIPSCDataPacket(pkt models.Packet, ss *streamState, dataType enums.DataType) []byte {
	slot := enums.Slot1
	if pkt.Slot {
		slot = enums.Slot2
	}
	var b ipscwire.User
	res, err := b.SetRawBurst(layer2.RawBurst(pkt.DMRData), layer2.Hint{Slot: slot, Kind: layer2.KindData, DataType: dataType, HasDataType: true})
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build data packet", "dtype", dataType, "error", err)
		return nil
	}
	if res.Uncorrectable && !t.rawBlocksAnXPRDrops {
		t.undeliverable(pkt, reasonFEC)
		return nil
	}
	u := t.user(pkt, ss, true, false, ss.firstPacket)
	u.Burst, u.Data = b.Burst, b.Data
	ss.ipscSeq++
	return encodeUser(&u)
}

// buildRawBlock builds an undecodable block (0x1F) of the 196 information
// bits of pkt, with the slot type of dataType.
func (t *IPSCTranslator) buildRawBlock(pkt models.Packet, ss *streamState, dataType enums.DataType) []byte {
	raw := layer2.RawBurst(pkt.DMRData)
	d := ipscwire.Payload{Status: 0x40, HasBody: true, Flags: [2]byte{0x80, 0x0A}, TrailerLen: 4, Words: 17, Bits: 196}
	if pkt.Slot {
		d.Status |= 0x80
	}
	info := raw.Info()
	copy(d.Octets[:], info[:])
	st := raw.SlotType()
	st.DataType = dataType
	sti, err := st.Info()
	if err != nil {
		t.undeliverable(pkt, reasonSlotType)
		return nil
	}
	d.Trailer[1] = byte(sti)
	u := t.user(pkt, ss, true, false, ss.firstPacket)
	u.Burst, u.Data = ipscwire.BurstRaw, d
	ss.ipscSeq++
	return encodeUser(&u)
}

// buildVoiceBurst builds an IPSC voice burst packet: the 52-octet form for
// burst A, 66 octets with the Full LC of the call for burst E, and 57 for
// the others. The position comes from the DMRD packet.
func (t *IPSCTranslator) buildVoiceBurst(pkt models.Packet, ss *streamState) []byte {
	raw := layer2.RawBurst(pkt.DMRData)
	if err := t.burst.DecodeFromBytes(raw); err != nil || t.burst.Kind == layer2.KindData {
		slog.Debug("IPSCTranslator: skipping data burst in voice stream")
		return nil
	}

	d := pkt.Data()
	pos := d.Hint(0).VoiceBurst
	if _, ok := pos.Index(); !ok {
		pos, _ = enums.VoiceBurstAt(ss.burstIndex % 6)
	}
	idx, _ := pos.Index()
	ss.burstIndex = (idx + 1) % 6

	if !ss.haveLC {
		ss.lc, ss.haveLC = extractFullLCBytes(pkt), true
	}
	u := t.user(pkt, ss, false, false, false)
	u.Voice.LC = ss.lc
	if _, err := u.SetRawBurst(raw, layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}); err != nil {
		slog.Debug("IPSCTranslator: cannot build voice burst", "error", err)
		return nil
	}
	return encodeUser(&u)
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
	lc := extractFullLCBytes(pkt)
	cw, err := lc.Codeword(dataType)
	if err != nil {
		slog.Error("Failed to encode Full LC codeword", "error", err)
	}
	return cw
}

// fullLCMatchesPacket reports whether a voice Full LC names the same call as pkt.
func fullLCMatchesPacket(cw [12]byte, pkt models.Packet) bool {
	lc := pdu.LCBytes(cw[:9])
	group, target, source, ok := lc.Call()
	return ok && group == pkt.GroupCall && uint(target) == pkt.Dst && uint(source) == pkt.Src
}

// reverseStreamState tracks per-call state for IPSC→MMDVM translation.
type reverseStreamState struct {
	streamID     uint32
	ended        bool
	seq          uint8
	burstIndex   int             // 0-5 → A-F, when the Receiver cannot place a burst
	started      bool            // whether we've seen a voice header
	lastActivity time.Time       // tracks when this stream was last active
	rx           *voice.Receiver // places the voice bursts of the call
}

// TranslateToMMDVM converts raw IPSC user packet data into MMDVM DMRD Packets.
// Returns nil if the packet cannot be translated.
func (t *IPSCTranslator) TranslateToMMDVM(packetType byte, data []byte) []models.Packet {
	t.mu.Lock()
	defer t.mu.Unlock()

	u, err := ipscwire.DecodeUser(data)
	if err != nil || byte(u.Opcode) != packetType {
		slog.Debug("IPSCTranslator: ignoring IPSC packet", "type", packetType, "length", len(data), "error", err)
		return nil
	}

	groupCall := u.Opcode == ipscwire.OpGroupVoice || u.Opcode == ipscwire.OpGroupData
	slog.Debug("IPSCTranslator: TranslateToMMDVM",
		"packetType", u.Opcode, "src", u.Source, "dst", u.Destination, "groupCall", groupCall,
		"slot", u.Info.Slot(), "isEnd", u.Info.End())

	// Use call control as the stream identifier
	rss, ok := t.reverseStreams[u.CallControl]
	if ok && rss.ended && time.Since(rss.lastActivity) > endedCallHold {
		ok = false
	}
	if !ok {
		t.nextStreamID++
		if t.nextStreamID == 0 {
			t.nextStreamID = 1
		}
		rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1, Scratch: t.burst})
		if err != nil {
			slog.Error("IPSCTranslator: cannot make voice receiver", "error", err)
			return nil
		}
		rss = &reverseStreamState{
			streamID:     t.nextStreamID,
			lastActivity: time.Now(),
			rx:           rx,
		}
		t.reverseStreams[u.CallControl] = rss
	}
	rss.lastActivity = time.Now()

	base := models.Packet{
		Signature: string(dmrconst.CommandDMRD),
		Src:       uint(u.Source),
		Dst:       uint(u.Destination),
		Repeater:  uint(u.ID),
		Slot:      u.Info.Slot() == enums.Slot2,
		GroupCall: groupCall,
		StreamID:  uint(rss.streamID),
	}

	var results []models.Packet
	dt, isData := u.Burst.DataType()
	switch {
	case u.IsVoice():
		results = t.voiceToMMDVM(&u, base, rss)
	case u.Burst.Type() == ipscwire.BurstRaw:
		results = t.rawToMMDVM(&u, base, rss)
	case u.Burst.Type() == ipscwire.BurstSyncLost:
		t.events = rss.rx.Lost(time.Since(t.epoch), t.events[:0])
	case isData:
		results = t.dataToMMDVM(&u, dt, base, rss)
	default:
		slog.Debug("IPSCTranslator: unknown IPSC burst type", "burstType", u.Burst)
		return nil
	}

	if u.Info.End() || (isData && dt == enums.DataTypeTerminatorWithLC) {
		rss.ended = true
	}

	return results
}

// endedCallHold is how long a block with the call control of an ended call
// stays on the stream of that call. An XPR 8400 sends the raw block
// (0x1F) of a rate 1 data call after the packet with the end bit.
const endedCallHold = 2 * time.Second

// dataToMMDVM builds the MMDVM packet of an IPSC voice header, terminator
// or data burst. Only the first of the voice headers that IPSC repeats goes
// out.
func (t *IPSCTranslator) dataToMMDVM(u *ipscwire.User, dt enums.DataType, base models.Packet, rss *reverseStreamState) []models.Packet {
	if dt.CarriesFullLC() && !fixFullLC(u, dt, base.GroupCall) {
		return nil
	}
	raw, err := u.RawBurst(0, layer2.SyncSourceBS)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build data burst", "dtype", dt, "error", err)
		return nil
	}
	now := time.Since(t.epoch)
	t.events = rss.rx.Burst(now, &raw, u.Hint(now), t.events[:0])

	if dt == enums.DataTypeVoiceLCHeader {
		if rss.started {
			return nil
		}
		rss.started = true
		rss.burstIndex = 0
	}

	pkt := base
	pkt.Seq = uint(rss.seq)
	pkt.FrameType = dmrconst.FrameDataSync
	pkt.DTypeOrVSeq = uint(dt)
	pkt.DMRData = raw
	rss.seq++
	return []models.Packet{pkt}
}

// rawToMMDVM builds the MMDVM packet of an undecodable IPSC data block: its
// 196 information bits as the repeater sent them, with the data type of its
// slot type.
func (t *IPSCTranslator) rawToMMDVM(u *ipscwire.User, base models.Packet, rss *reverseStreamState) []models.Packet {
	raw, err := u.RawBurst(0, layer2.SyncSourceBS)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build undecodable data block", "error", err)
		return nil
	}
	now := time.Since(t.epoch)
	h := u.Hint(now)
	t.events = rss.rx.Burst(now, &raw, h, t.events[:0])

	pkt := base
	pkt.Seq = uint(rss.seq)
	pkt.FrameType = dmrconst.FrameDataSync
	pkt.DTypeOrVSeq = uint(h.DataType)
	pkt.DMRData = raw
	rss.seq++
	return []models.Packet{pkt}
}

// fixFullLC makes the Full LC of an IPSC voice header or terminator name
// the call type of the IPSC packet, and builds the LC from the packet
// fields when the packet has none. The parity covers the FLCO, so it
// reseals after a change. It reports false when the LC cannot be sealed.
func fixFullLC(u *ipscwire.User, dt enums.DataType, groupCall bool) bool {
	var payload [12]byte
	reseal := false
	if u.Data.Bits == 96 {
		copy(payload[:], u.Data.Octets[:12])
	} else {
		lc := extractFullLCBytes(models.Packet{Src: uint(u.Source), Dst: uint(u.Destination), GroupCall: groupCall})
		copy(payload[:9], lc[:])
		reseal = true
	}

	flco := enums.FLCOUnitToUnitVoiceChannelUser
	if groupCall {
		flco = enums.FLCOGroupVoiceChannelUser
	}
	if lc := (*pdu.LCBytes)(payload[:9]); lc.FLCO() != flco {
		if err := lc.SetFLCO(flco); err != nil {
			slog.Debug("IPSCTranslator: cannot set FLCO", "error", err)
			return false
		}
		reseal = true
	}
	if reseal {
		if err := pdu.Seal(&payload, dt); err != nil {
			slog.Debug("IPSCTranslator: cannot seal Full LC", "error", err)
			return false
		}
	}
	copy(u.Data.Octets[:12], payload[:])
	u.Data.Bits = 96
	return true
}

// voiceToMMDVM builds the MMDVM packet of an IPSC voice burst. The burst
// keeps the EMB and embedded data of the IPSC packet, and the voice
// Receiver of the call places it in the superframe.
func (t *IPSCTranslator) voiceToMMDVM(u *ipscwire.User, base models.Packet, rss *reverseStreamState) []models.Packet {
	raw, err := u.RawBurst(0, layer2.SyncSourceMS)
	if err != nil {
		slog.Debug("IPSCTranslator: cannot build voice burst", "error", err)
		return nil
	}
	now := time.Since(t.epoch)
	t.events = rss.rx.Burst(now, &raw, u.Hint(now), t.events[:0])

	var results []models.Packet
	for i := range t.events {
		ev := &t.events[i]
		if ev.Kind != voice.EventVoice {
			continue
		}
		idx, ok := ev.Position.Index()
		if !ok {
			idx = rss.burstIndex % 6
		}
		rss.burstIndex = (idx + 1) % 6

		pkt := base
		pkt.Seq = uint(rss.seq)
		pkt.FrameType = dmrconst.FrameVoice
		if idx == 0 {
			pkt.FrameType = dmrconst.FrameVoiceSync
		}
		pkt.DTypeOrVSeq = uint(idx) //nolint:gosec // idx is 0 to 5
		pkt.DMRData = ev.Burst
		rss.seq++
		results = append(results, pkt)
	}
	return results
}
