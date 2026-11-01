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

// Package storeforward holds the private text messages that the hub could
// not deliver to the radios of users who turned store and forward on, and
// sends them when the radio is heard again. It sends a held text only in
// the format that the radio itself sends texts in, which it learns from the
// last private text that the radio sent, as the wrong format can lock up a
// radio.
package storeforward

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

const headerOctets = 12

var (
	// ErrNoFormat is returned for a template that is not a private text
	// that the hub can read the format of: a group text, a text with a bad
	// CRC or checksum, a text stored without its rate, or one with a P_HEAD.
	ErrNoFormat = errors.New("storeforward: the template is not a text of a known format")
	// ErrNotReproducible is returned for a template whose own text the hub
	// cannot build again octet for octet, so that the hub does not know how
	// to build another text in that format.
	ErrNotReproducible = errors.New("storeforward: the format of the template cannot be built exactly")
	// ErrText is returned for a text that the format of the template
	// cannot carry.
	ErrText = errors.New("storeforward: the text does not fit the format of the template")
)

// Format is the text format of one radio, read from a text that it sent.
type Format struct {
	header  pdu.DataHeader
	payload []byte
	rate    enums.DataType
	text    sms.Text
	nul     bool
	dgram   ip.Datagram
	src     uint32
	dst     uint32
}

// FormatOf reads the format of the private text tpl that a radio sent, and
// checks that building the text of tpl in it gives the octets of tpl. It
// returns ErrNoFormat or ErrNotReproducible when it cannot.
func FormatOf(tpl *models.Message) (*Format, error) {
	if tpl == nil || tpl.IsGroup || tpl.BadCRC || !tpl.ChecksumOK || tpl.Proprietary || len(tpl.Raw) <= headerOctets {
		return nil, ErrNoFormat
	}
	rate := enums.DataType(tpl.Rate)
	if rate != enums.DataTypeRate12 && rate != enums.DataTypeRate34 && rate != enums.DataTypeRate1 {
		return nil, ErrNoFormat
	}
	f := &Format{
		header:  pdu.DecodeDataHeader([12]byte(tpl.Raw[:headerOctets])),
		payload: bytes.Clone(tpl.Raw[headerOctets:]),
		rate:    rate,
	}
	dst, src, ok := f.header.Addresses()
	if !ok {
		return nil, ErrNoFormat
	}
	f.src, f.dst = src, dst
	body, t, err := sms.Decode(nil, &f.header, f.payload)
	if err != nil || t.Group {
		return nil, ErrNoFormat
	}
	f.text = t
	f.text.Body = nil
	switch t.Format {
	case sms.FormatTMS, sms.FormatIP, sms.FormatAnyTone:
		sap, _ := f.header.SAP()
		if sap != enums.SAPIPBasedPacketData {
			return nil, ErrNotReproducible
		}
		if f.dgram, err = ip.Decode(sap, f.payload); err != nil {
			return nil, ErrNoFormat
		}
		p := f.dgram.Payload
		f.nul = len(p) >= 2 && p[len(p)-2] == 0 && p[len(p)-1] == 0
		f.dgram.Payload = nil
	case sms.FormatDefinedData, sms.FormatHytera:
	default:
		return nil, ErrNoFormat
	}
	own, err := f.build(body, src, dst)
	if err != nil || !bytes.Equal(own.Payload, f.payload) {
		return nil, ErrNotReproducible
	}
	p, err := packet.NewPacket(own.Header, own.Payload, rate)
	if err != nil {
		return nil, ErrNotReproducible
	}
	got, err := p.Header.Encode()
	if err != nil || !bytes.Equal(got[:], tpl.Raw[:headerOctets]) {
		return nil, ErrNotReproducible
	}
	return f, nil
}

// Confirmed reports whether the radio asks for a response to its texts.
func (f *Format) Confirmed() bool {
	return f.text.Confirmed
}

// Name returns the name of the text format, such as "AnyTone".
func (f *Format) Name() string {
	return f.text.Format.String()
}

// Build returns the text body from src to dst in the format f, as a
// message for a data sender. It returns ErrText when the format cannot
// carry the text.
func (f *Format) Build(body string, src, dst uint) (data.Message, error) {
	if src > models.MaxDMRID || dst > models.MaxDMRID {
		return data.Message{}, fmt.Errorf("%w: radio ID out of range", ErrText)
	}
	m, err := f.build([]byte(body), uint32(src), uint32(dst)) //nolint:gosec // checked against MaxDMRID
	if err != nil {
		return data.Message{}, fmt.Errorf("%w: %w", ErrText, err)
	}
	return m, nil
}

func (f *Format) build(body []byte, src, dst uint32) (data.Message, error) {
	h, err := f.headerFor(src, dst)
	if err != nil {
		return data.Message{}, err
	}
	t := f.text
	t.Source, t.Destination, t.Group = src, dst, false
	t.Body = body
	if f.nul && t.Format != sms.FormatAnyTone {
		t.Body = append(bytes.Clone(body), 0)
	}
	var sh sms.Header
	built, err := sms.AppendPacket(nil, &t, &sh)
	if err != nil {
		return data.Message{}, fmt.Errorf("building the text: %w", err)
	}
	payload := built
	if t.Format == sms.FormatTMS || t.Format == sms.FormatIP || t.Format == sms.FormatAnyTone {
		d, err := ip.Decode(enums.SAPIPBasedPacketData, built)
		if err != nil {
			return data.Message{}, fmt.Errorf("reading the built datagram: %w", err)
		}
		app := d.Payload
		if t.Format == sms.FormatAnyTone && !f.nul {
			app = app[:len(app)-2]
		}
		out := f.dgram
		out.Source = ip.RadioAddress(t.Network, src)
		out.Destination = ip.RadioAddress(t.Network, dst)
		if t.Network == 0 {
			out.Source, out.Destination = ip.RadioAddress(ip.DefaultNetwork, src), ip.RadioAddress(ip.DefaultNetwork, dst)
		}
		out.Payload = app
		if payload, err = ip.AppendIPv4(nil, &out); err != nil {
			return data.Message{}, fmt.Errorf("building the datagram: %w", err)
		}
	}
	return data.Message{Header: h, Rate: f.rate, Payload: payload}, nil
}

func (f *Format) headerFor(src, dst uint32) (pdu.DataHeader, error) {
	h := f.header
	var err error
	switch {
	case h.Format() == pdu.FormatUnconfirmed:
		u, _ := h.AsUnconfirmed()
		u.LLIDSource, u.LLIDDestination, u.Group = src, dst, false
		err = h.SetUnconfirmed(u)
	case h.Format() == pdu.FormatConfirmed:
		c, _ := h.AsConfirmed()
		c.LLIDSource, c.LLIDDestination, c.Group = src, dst, false
		err = h.SetConfirmed(c)
	default:
		d, ok := h.AsDefinedData()
		if !ok {
			return h, ErrNotReproducible
		}
		d.LLIDSource, d.LLIDDestination, d.Group = src, dst, false
		err = h.SetDefinedData(d)
	}
	if err != nil {
		return h, fmt.Errorf("building the data header: %w", err)
	}
	return h, nil
}
