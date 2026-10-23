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

//go:build stress

package stress_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

// addr is where a kind goes: the lane's talkgroup or the user of another
// repeater.
type addr struct {
	group    bool
	dst, src uint
	want     []uint
}

func (r *rig) pickAddr(rnd *rand.Rand, i int, slot2 bool, private bool) addr {
	if private || rnd.IntN(2) == 0 {
		j := otherRepeater(rnd, r.top.n, i)
		return addr{dst: userID(j), src: userID(i), want: r.privateWant(rptID(j))}
	}
	g := r.lanesTG(i, slot2)
	return addr{group: true, dst: tgID(g), src: userID(i), want: r.groupWant(i, g)}
}

type kindMaker struct {
	name    string
	private bool
	make    func(a addr, rnd *rand.Rand) script
}

func kindScript(name, kind, sub string, a addr, b []burst, err error) script {
	return script{label: name, kind: kind, group: a.group, dst: a.dst, src: a.src, want: a.want, bursts: b, rowKind: kind, sub: sub, err: err}
}

func smsKind(name string, f sms.Format, rate enums.DataType) kindMaker {
	return kindMaker{name: name, make: func(a addr, _ *rand.Rand) script {
		s := smsScript(name, a.group, a.dst, a.src, a.want, f, rate)
		return s
	}}
}

func csbkKind(name, sub string, copies int, set func(cs *pdu.CSBK, a addr) error) kindMaker {
	return kindMaker{name: name, private: true, make: func(a addr, _ *rand.Rand) script {
		var cs pdu.CSBK
		if err := set(&cs, a); err != nil {
			return kindScript(name, "csbk", sub, a, nil, err)
		}
		raw, err := csbkRaw(&cs)
		var b []burst
		for range copies {
			b = append(b, dataBurst(raw))
		}
		return kindScript(name, "csbk", sub, a, b, err)
	}}
}

// kindMakers are the traffic kinds of PLAN section 3 that the MMDVM
// network protocol carries in DMRD packets.
func kindMakers() []kindMaker {
	return []kindMaker{
		{name: "voice", make: func(a addr, rnd *rand.Rand) script {
			return voiceCall("voice", a.group, a.dst, a.src, between(12, 40)(rnd), a.want)
		}},
		smsKind("sms-ip", sms.FormatIP, enums.DataTypeRate12),
		smsKind("sms-anytone", sms.FormatAnyTone, enums.DataTypeRate12),
		smsKind("sms-tms", sms.FormatTMS, enums.DataTypeRate12),
		smsKind("sms-dd", sms.FormatDefinedData, enums.DataTypeRate12),
		smsKind("sms-hytera", sms.FormatHytera, enums.DataTypeRate12),
		smsKind("sms-rate34", sms.FormatIP, enums.DataTypeRate34),
		smsKind("sms-rate1", sms.FormatIP, enums.DataTypeRate1),
		csbkKind("radio-check", "radio_check", 1, func(cs *pdu.CSBK, a addr) error {
			return cs.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(a.src), CheckedAddress: uint32(a.dst)}) //nolint:gosec
		}),
		csbkKind("call-alert", "call_alert", 3, func(cs *pdu.CSBK, a addr) error {
			return cs.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: uint32(a.dst), SourceAddress: uint32(a.src)}) //nolint:gosec
		}),
		{name: "mbc", private: true, make: func(a addr, _ *rand.Rand) script {
			h := pdu.MBCHeader{}
			if err := h.SetPrivateDataGrant(pdu.PrivateDataGrantPDU{PhysicalChannel: 0xFFF, TargetAddress: uint32(a.dst), SourceAddress: uint32(a.src)}); err != nil { //nolint:gosec
				return kindScript("mbc", "mbc", "", a, nil, err)
			}
			hp, err := h.Encode()
			if err != nil {
				return kindScript("mbc", "mbc", "", a, nil, err)
			}
			head, err := singleBPTC(enums.DataTypeMBCHeader, hp)
			if err != nil {
				return kindScript("mbc", "mbc", "", a, nil, err)
			}
			cont := pdu.MBCContinuation{LastBlock: true}
			cp, err := cont.Encode()
			if err != nil {
				return kindScript("mbc", "mbc", "", a, nil, err)
			}
			tail, err := singleBPTC(enums.DataTypeMBCContinuation, cp)
			return kindScript("mbc", "mbc", "", a, append(head, tail...), err)
		}},
		{name: "udt", make: func(a addr, _ *rand.Rand) script {
			var h pdu.DataHeader
			if err := h.SetUDT(pdu.UDTHeader{
				Group: a.group, SAP: enums.SAPShortData, UDTFormat: enums.UDTISO8,
				LLIDDestination: uint32(a.dst), LLIDSource: uint32(a.src), Opcode: pdu.UDTOpcodeInboundHeader, //nolint:gosec
			}); err != nil {
				return kindScript("udt", "udt", "", a, nil, err)
			}
			hp, err := h.Encode()
			if err != nil {
				return kindScript("udt", "udt", "", a, nil, err)
			}
			head, err := singleBPTC(enums.DataTypeDataHeader, hp)
			if err != nil {
				return kindScript("udt", "udt", "", a, nil, err)
			}
			block, err := singleBPTC(enums.DataTypeRate12, [12]byte{'U', 'D', 'T', ' ', 'S', 'T', 'R', 'E', 'S', 'S'})
			return kindScript("udt", "udt", "", a, append(head, block...), err)
		}},
		{name: "usbd", make: func(a addr, _ *rand.Rand) script {
			u := pdu.UnifiedSingleBlockData{ServiceType: pdu.ServiceTypeManufacturerSpecific1, Payload: [10]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}}
			p, err := u.Encode()
			if err != nil {
				return kindScript("usbd", "usbd", "", a, nil, err)
			}
			b, err := singleBPTC(enums.DataTypeUnifiedSingleBlock, p)
			return kindScript("usbd", "usbd", "", a, b, err)
		}},
		{name: "idle", make: func(a addr, _ *rand.Rand) script {
			b, err := singleBPTC(enums.DataTypeIdle, pdu.PRFillBits())
			return kindScript("idle", "idle", "", a, b, err)
		}},
		{name: "random-octets", make: func(a addr, rnd *rand.Rand) script {
			return kindScript("random-octets", "unknown", "", a, []burst{randomBurst(rnd, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK))}, nil)
		}},
		{name: "frame-type-3", make: func(a addr, rnd *rand.Rand) script {
			return kindScript("frame-type-3", "unknown", "", a, []burst{randomBurst(rnd, dmrconst.FrameType(3), 0)}, nil)
		}},
	}
}

// TestEveryKind mixes every traffic kind on the same slots of every
// repeater, with confirmed SMS answered by the target repeater and talker
// alias and GPS packets in between, and checks that every spec-valid
// burst is forwarded unchanged to its targets once, with a row of the
// right kind and a message row for each SMS.
func TestEveryKind(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "kinds")
	r := newRig(t, s, res, logs, rigOptions{withOB: true})
	col := &collector{}
	r.led.onDone = col.add

	makers := kindMakers()
	m := make(mix, 0, len(makers))
	for _, k := range makers {
		m = append(m, struct {
			weight int
			make   func(r *rig, rnd *rand.Rand, i int, slot2 bool) script
		}{3, func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
			return k.make(r.pickAddr(rnd, i, slot2, k.private), rnd)
		}})
	}
	var extra sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), s.Duration)
	defer cancel()
	extra.Add(2)
	go func() {
		defer extra.Done()
		r.confirmedExchanges(ctx)
	}()
	go func() {
		defer extra.Done()
		r.aliasAndPosition(ctx)
	}()
	r.runLanes(ctx, s.Duration, m, s.Duty, 0, nil)
	r.waitLanes()
	extra.Wait()
	time.Sleep(8 * time.Second)
	r.led.finish(0, true)

	r.checkDelivery()
	calls := col.all()
	r.checkRows(calls)
	r.checkMessages(calls)
	r.checkNoStuckStreams(15 * time.Second)
	byKind := map[string]int{}
	for _, c := range calls {
		byKind[c.label]++
	}
	res.extra("transmissions_by_kind", byKind)
}

// confirmedExchanges sends confirmed private SMS at rates 1/2, 3/4 and 1
// and has the target repeater answer with an ACK, and sometimes first
// with a NACK and a retry.
func (r *rig) confirmedExchanges(ctx context.Context) {
	rnd := rand.New(rand.NewPCG(7, 7)) //nolint:gosec
	rates := []enums.DataType{enums.DataTypeRate12, enums.DataTypeRate34, enums.DataTypeRate1}
	for n := 0; ctx.Err() == nil; n++ {
		i := rnd.IntN(r.top.n)
		j := otherRepeater(rnd, r.top.n, i)
		slot2 := rnd.IntN(2) == 0
		rate := rates[n%len(rates)]
		text := fmt.Sprintf("CONFIRMED %d", smsCounter.Add(1))
		b, p, err := smsBursts(sms.FormatIP, false, userID(j), userID(i), text, rate, true)
		label := fmt.Sprintf("sms-confirmed-%v", rate)
		s := script{label: label, kind: "data", dst: userID(j), src: userID(i), want: r.privateWant(rptID(j)), bursts: b, rowKind: "data", text: text, confirm: true, err: err}
		nack := n%4 == 3
		if nack {
			s.text = ""
		}
		r.play(i, slot2, s)
		if p == nil {
			continue
		}
		if nack {
			rb, err := responseBursts(p, packet.ResponseNACKPacketCRC)
			r.play(j, slot2, script{label: "nack", kind: "data", dst: userID(i), src: userID(j), want: r.privateWant(rptID(i)), bursts: rb, err: err})
			s.text = text
			s.label = label + "-retry"
			r.play(i, slot2, s)
		}
		rb, err := responseBursts(p, packet.ResponseACK)
		r.play(j, slot2, script{label: "ack", kind: "data", dst: userID(i), src: userID(j), want: r.privateWant(rptID(i)), bursts: rb, err: err})
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(500+rnd.IntN(1500)) * time.Millisecond):
		}
	}
}

// aliasAndPosition sends talker alias and GPS packets from random
// repeaters while the lanes run.
func (r *rig) aliasAndPosition(ctx context.Context) {
	rnd := rand.New(rand.NewPCG(9, 9)) //nolint:gosec
	lcs, err := pdu.EncodeTalkerAlias("STRESS", layer3.TalkerAliasSevenBit)
	if err != nil {
		r.res.fail(r.t, "talker alias: %v", err)
		return
	}
	var gps pdu.FullLinkControl
	if err := gps.SetGPSInfo(pdu.FLCGPSInfo{Latitude: 32.75, Longitude: -97.25, PositionError: layer3.PositionLessThan2M}); err != nil {
		r.res.fail(r.t, "GPS: %v", err)
		return
	}
	for ctx.Err() == nil {
		i := rnd.IntN(r.top.n)
		c := r.clients[i]
		for k := range lcs {
			a := dmrd.TalkerAlias{Repeater: uint32(rptID(i)), Source: uint32(userID(i)), Type: uint8(k), Data: lcs[k].RawPayload, Size: 19} //nolint:gosec
			if p, err := a.AppendBinary(nil); err == nil {
				_ = c.SendRaw(p)
				r.res.inc("alias_packets")
			}
		}
		pos := dmrd.Position{Repeater: uint32(rptID(i)), Source: uint32(userID(i)), Data: gps.RawPayload, Size: 18} //nolint:gosec
		if p, err := pos.AppendBinary(nil); err == nil {
			_ = c.SendRaw(p)
			r.res.inc("gps_packets")
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(200+rnd.IntN(800)) * time.Millisecond):
		}
	}
}
