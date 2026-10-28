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

package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
)

const (
	burstGap    = 60 * time.Millisecond
	steppedGap  = 250 * time.Millisecond
	colorCode   = 1
	handshake   = 10 * time.Second
	readTimeout = 200 * time.Millisecond
)

type site struct {
	callsign    string
	rx, tx      uint32
	lat, lon    float64
	location    string
	description string
	slots       uint8
}

type peer struct {
	id   uint32
	conn *net.UDPConn
	done chan struct{}
	wg   sync.WaitGroup
	mu   sync.Mutex
}

func dialPeer(addr string, id uint32, password string, s site) (*peer, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("bad address %s: %w", addr, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	p := &peer{id: id, conn: conn, done: make(chan struct{})}
	if err := p.login(password, s); err != nil {
		_ = conn.Close()
		return nil, err
	}
	p.wg.Add(1)
	go p.drain()
	return p, nil
}

func (p *peer) control(c dmrd.Command, token [32]byte) error {
	b, err := (&dmrd.Control{Command: c, ID: p.id, Token: token}).AppendBinary(nil, dmrd.DialectMMDVM)
	if err != nil {
		return fmt.Errorf("could not build %v: %w", c, err)
	}
	return p.write(b)
}

func (p *peer) write(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.conn.Write(b); err != nil {
		return fmt.Errorf("could not send: %w", err)
	}
	return nil
}

func (p *peer) ack() (dmrd.Control, error) {
	buf := make([]byte, 512)
	deadline := time.Now().Add(handshake)
	for time.Now().Before(deadline) {
		_ = p.conn.SetReadDeadline(time.Now().Add(readTimeout))
		n, err := p.conn.Read(buf)
		if err != nil {
			continue
		}
		switch dmrd.Classify(buf[:n], dmrd.DialectMMDVM) { //nolint:exhaustive
		case dmrd.CommandRPTACK:
			msg, err := dmrd.DecodeControl(buf[:n], dmrd.DialectMMDVM)
			if err != nil {
				return dmrd.Control{}, fmt.Errorf("bad RPTACK: %w", err)
			}
			return msg, nil
		case dmrd.CommandMSTNAK:
			return dmrd.Control{}, fmt.Errorf("repeater %d: the hub refused the login", p.id)
		}
	}
	return dmrd.Control{}, fmt.Errorf("repeater %d: login: %w", p.id, errTimeout)
}

func (p *peer) login(password string, s site) error {
	if err := p.control(dmrd.CommandRPTL, [32]byte{}); err != nil {
		return err
	}
	msg, err := p.ack()
	if err != nil {
		return err
	}
	salt, ok := msg.LoginSalt()
	if !ok {
		return fmt.Errorf("repeater %d: no salt in the login answer", p.id)
	}
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, salt, []byte(password))
	if err != nil {
		return fmt.Errorf("could not make the login token: %w", err)
	}
	if err := p.control(dmrd.CommandRPTK, token); err != nil {
		return err
	}
	if _, err := p.ack(); err != nil {
		return err
	}
	var cfg dmrd.MMDVMConfig
	cfg.Reset()
	cfg.SetID(p.id)
	if err := errors.Join(
		cfg.SetCallsign([]byte(s.callsign)),
		cfg.SetRXFrequency(s.rx),
		cfg.SetTXFrequency(s.tx),
		cfg.SetPower(1),
		cfg.SetColorCode(colorCode),
		cfg.SetLatitude(fmt.Appendf(nil, "%+08.4f", s.lat)),
		cfg.SetLongitude(fmt.Appendf(nil, "%+09.4f", s.lon)),
		cfg.SetHeight(10),
		cfg.SetLocation([]byte(s.location)),
		cfg.SetDescription([]byte(s.description)),
		cfg.SetSlots(s.slots),
		cfg.SetURL([]byte("https://example.com")),
		cfg.SetSoftwareID([]byte("20260101")),
		cfg.SetPackageID([]byte("MMDVM_DEMO")),
	); err != nil {
		return fmt.Errorf("could not build the configuration: %w", err)
	}
	if err := p.write(cfg[:]); err != nil {
		return err
	}
	_, err = p.ack()
	return err
}

func (p *peer) drain() {
	defer p.wg.Done()
	buf := make([]byte, 512)
	for {
		select {
		case <-p.done:
			return
		default:
			_ = p.conn.SetReadDeadline(time.Now().Add(readTimeout))
			_, _ = p.conn.Read(buf)
		}
	}
}

func (p *peer) ping() error {
	return p.control(dmrd.CommandRPTPING, [32]byte{})
}

func (p *peer) close() {
	close(p.done)
	p.wg.Wait()
	_ = p.control(dmrd.CommandRPTCL, [32]byte{})
	_ = p.conn.Close()
}

type burst struct {
	frame dmrconst.FrameType
	value uint
	raw   layer2.RawBurst
}

type transmission struct {
	src, dst uint
	group    bool
	slot2    bool
	bursts   []burst
}

func (p *peer) play(t transmission, stream uint, at func(k int) error) error {
	gap := burstGap
	if at != nil {
		gap = steppedGap
	}
	next := time.Now()
	for k, b := range t.bursts {
		time.Sleep(time.Until(next))
		if at != nil {
			if err := at(k); err != nil {
				return err
			}
		}
		next = next.Add(gap)
		pkt := models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(k & 0xFF), Src: t.src, Dst: t.dst, Repeater: uint(p.id),
			Slot: t.slot2, GroupCall: t.group, FrameType: b.frame, DTypeOrVSeq: b.value, StreamID: stream,
			DMRData: b.raw, BER: 0, RSSI: 63 + k%5,
		}
		if err := p.write(pkt.Encode()); err != nil {
			return err
		}
	}
	return nil
}

func voiceLC(group bool, dst, src uint, dt enums.DataType) (burst, error) {
	lc := pdu.FullLinkControl{BurstDataType: dt}
	var err error
	if group {
		err = lc.SetGroupVoice(pdu.FLCGroupVoice{GroupAddress: uint32(dst), SourceAddress: uint32(src)})
	} else {
		err = lc.SetUnitToUnit(pdu.FLCUnitToUnit{TargetAddress: uint32(dst), SourceAddress: uint32(src)})
	}
	if err != nil {
		return burst{}, fmt.Errorf("voice LC: %w", err)
	}
	raw, err := layer2.BuildBurst(&lc, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return burst{}, fmt.Errorf("voice LC burst: %w", err)
	}
	value := uint(dmrconst.DTypeVoiceHead)
	if dt == enums.DataTypeTerminatorWithLC {
		value = uint(dmrconst.DTypeVoiceTerm)
	}
	return burst{frame: dmrconst.FrameDataSync, value: value, raw: raw}, nil
}

func voiceCall(group bool, dst, src uint, frames int) ([]burst, error) {
	head, err := voiceLC(group, dst, src, enums.DataTypeVoiceLCHeader)
	if err != nil {
		return nil, err
	}
	out := []burst{head, head}
	silence := vocoder.AMBESilence()
	for i := range frames {
		pos := i % 6
		vb, _ := enums.VoiceBurstAt(pos)
		raw, err := layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{silence, silence, silence}, vb,
			pdu.EmbeddedSignalling{ColorCode: colorCode}, [4]byte{}, layer2.SyncSourceMS)
		if err != nil {
			return nil, fmt.Errorf("voice burst: %w", err)
		}
		f := dmrconst.FrameVoice
		if pos == 0 {
			f = dmrconst.FrameVoiceSync
		}
		out = append(out, burst{frame: f, value: uint(pos), raw: raw})
	}
	term, err := voiceLC(group, dst, src, enums.DataTypeTerminatorWithLC)
	if err != nil {
		return nil, err
	}
	return append(out, term), nil
}

func dataBurst(raw layer2.RawBurst) burst {
	return burst{frame: dmrconst.FrameDataSync, value: uint(raw.SlotType().DataType), raw: raw}
}

func preambles(group bool, dst, src uint, n, follow int) ([]burst, error) {
	out := make([]burst, 0, n)
	for i := range n {
		var cs pdu.CSBK
		if err := cs.SetPreamble(pdu.PreamblePDU{
			Data: true, Group: group, CSBKBlocksToFollow: byte(follow + n - 1 - i),
			TargetAddress: uint32(dst), SourceAddress: uint32(src),
		}); err != nil {
			return nil, fmt.Errorf("preamble: %w", err)
		}
		cs.LastBlock = true
		p, err := cs.Encode()
		if err != nil {
			return nil, fmt.Errorf("preamble: %w", err)
		}
		raw, err := layer2.BuildDataBurst(p, enums.DataTypeCSBK, colorCode, layer2.SyncSourceMS)
		if err != nil {
			return nil, fmt.Errorf("preamble burst: %w", err)
		}
		out = append(out, dataBurst(raw))
	}
	return out, nil
}

func packetBursts(group bool, dst, src uint, p *packet.Packet) ([]burst, error) {
	raws, err := packet.Bursts(p, colorCode, layer2.SyncSourceMS)
	if err != nil {
		return nil, fmt.Errorf("bursts: %w", err)
	}
	out, err := preambles(group, dst, src, 3, len(raws))
	if err != nil {
		return nil, err
	}
	for _, r := range raws {
		out = append(out, dataBurst(r))
	}
	return out, nil
}

func textMessage(group bool, dst, src uint, body string) ([]burst, error) {
	text := &sms.Text{Format: sms.FormatIP, Source: uint32(src), Destination: uint32(dst), Group: group, Body: []byte(body)}
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	if err != nil {
		return nil, fmt.Errorf("text message: %w", err)
	}
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	if err != nil {
		return nil, fmt.Errorf("text message packet: %w", err)
	}
	return packetBursts(group, dst, src, &p)
}

func locationReport(radio, service uint, lat, lon float64, requestID uint32) ([]burst, error) {
	m := lrrp.Message{Document: lrrp.DocumentTriggeredReport}
	m.Report.RequestID = []byte{byte(requestID >> 24), byte(requestID >> 16), byte(requestID >> 8), byte(requestID)}
	m.Report.HasRequestID = true
	m.Report.Circle2D = lrrp.Circle2D{Latitude: lat, Longitude: lon, Radius: 8}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	if err != nil {
		return nil, fmt.Errorf("location report: %w", err)
	}
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, uint32(radio)),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, uint32(service)),
		SourcePort:      ip.PortLRRP,
		DestinationPort: ip.PortLRRP,
		TTL:             64,
		Payload:         payload,
	}
	dg, err := ip.AppendIPv4(nil, &d)
	if err != nil {
		return nil, fmt.Errorf("location datagram: %w", err)
	}
	var h pdu.DataHeader
	if err := h.SetUnconfirmed(pdu.UnconfirmedDataHeader{
		SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(service), LLIDSource: uint32(radio), FullMessageFlag: true,
	}); err != nil {
		return nil, fmt.Errorf("location header: %w", err)
	}
	p, err := packet.NewPacket(h, dg, enums.DataTypeRate12)
	if err != nil {
		return nil, fmt.Errorf("location packet: %w", err)
	}
	return packetBursts(false, service, radio, &p)
}
