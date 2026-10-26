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
	"errors"
	"math"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
)

const (
	// preambleGap is how long a call that ends with a preamble CSBK takes
	// the CSBK or data header that the preamble announces on another
	// stream, as MMDVMHost sends them. An XPR 8400 sends the packets of a
	// call with preambles at most 180 ms apart; twice that allows for
	// network jitter.
	preambleGap   = 360 * time.Millisecond
	maxDataBlocks = 127
)

// dataEnd follows the data packet of a call to find its last block.
type dataEnd struct {
	asm *packet.Assembler
	// pHead is set after a first header with the proprietary SAP, which a
	// P_HEAD should follow.
	pHead bool
}

// decodeDataBurst returns the burst of pkt when it is a CSBK, a data
// header or a rate 1/2, 3/4 or 1 block that decodes, and nil otherwise.
func decodeDataBurst(pkt models.Packet) *layer2.Burst {
	if pkt.FrameType != dmrconst.FrameDataSync || pkt.DTypeOrVSeq > math.MaxUint8 {
		return nil
	}
	switch enums.DataType(pkt.DTypeOrVSeq) { //nolint:exhaustive
	case enums.DataTypeCSBK, enums.DataTypeDataHeader, enums.DataTypeRate12, enums.DataTypeRate34, enums.DataTypeRate1:
	default:
		return nil
	}
	b, err := layer2.NewBurstFromBytes(pkt.DMRData)
	if err != nil || b.Kind != layer2.KindData || b.Data == nil {
		return nil
	}
	return b
}

// isPreamble reports whether b is a preamble CSBK (ETSI TS 102 361-2
// 7.1.2.5).
func isPreamble(b *layer2.Burst) bool {
	cs, ok := b.Data.(*pdu.CSBK)
	return ok && !cs.FEC.Uncorrectable && cs.FID == enums.FIDStandardized && cs.Opcode == pdu.CSBKPreamble
}

// ends adds b, a CSBK, data header or data block of the call, to d and
// reports whether b ends the call, and false for known when b is a data
// block with no data packet in progress, which tells nothing. A data header with no blocks to follow
// and the last block of a data packet end it, as does a packet whose CRC
// fails; a block that does not decode still counts. A CSBK that is a
// preamble or the last block ends it when no data packet is in progress.
//
// The Blocks To Follow of a first header with SAP 9 counts the P_HEAD that
// follows it (ETSI TS 102 361-1 8.2.1 and 8.2.2.1). MMDVMHost does not pass
// the P_HEAD of an RF packet to the network, so when a data block follows
// such a header directly, the P_HEAD is counted as if it had arrived.
func (d *dataEnd) ends(b *layer2.Burst) (end, known bool) {
	_, header := b.Data.(*pdu.DataHeader)
	if cs, ok := b.Data.(*pdu.CSBK); ok {
		return !cs.FEC.Uncorrectable && (cs.LastBlock || isPreamble(b)) && !d.asm.InProgress(), true
	}
	if !header && !d.asm.InProgress() {
		return false, false
	}
	if d.asm == nil {
		d.asm = packet.NewAssembler(maxDataBlocks)
	}
	pHead := d.pHead
	d.pHead = false
	switch h := b.Data.(type) {
	case *pdu.DataHeader:
		d.pHead = announcesPHead(h)
	case *pdu.Rate12Data, *pdu.Rate34Data, *pdu.Rate1Data:
		if pHead {
			var p pdu.DataHeader
			if p.SetProprietary(pdu.ProprietaryHeader{SAP: enums.SAPProprietaryPacketData}) == nil {
				_, _, _ = d.asm.Add(&p)
			}
		}
	}
	_, done, err := d.asm.AddBurst(b)
	return done || errors.Is(err, packet.ErrBlocksMissing), true
}

// announcesPHead reports whether h is the first header of a proprietary
// packet: an unconfirmed or confirmed data header with SAP 9.
func announcesPHead(h *pdu.DataHeader) bool {
	if u, ok := h.AsUnconfirmed(); ok {
		return u.SAP == enums.SAPProprietaryPacketData
	}
	if c, ok := h.AsConfirmed(); ok {
		return c.SAP == enums.SAPProprietaryPacketData
	}
	return false
}

// joins reports whether pkt, with the data burst b, of a stream that has
// no call on the slot, belongs to the call c: c ends with a preamble CSBK
// less than preambleGap before now, and pkt is the CSBK or data header that
// the preamble announces, from the same source to the same target.
func (c *queuedCall) joins(pkt models.Packet, b *layer2.Burst, now time.Time) bool {
	if !c.preamble || b == nil || now.Sub(c.lastSeen) >= preambleGap {
		return false
	}
	switch b.Data.(type) {
	case *pdu.CSBK, *pdu.DataHeader:
	default:
		return false
	}
	return pkt.Src == c.src && pkt.Dst == c.dst && pkt.GroupCall == c.group
}
