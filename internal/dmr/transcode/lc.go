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

package transcode

import (
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
)

// withFID returns the Voice LC Header or Terminator with LC raw of data
// type dt with the FID of its LC set to fid. ok is false when the LC of
// raw does not pass its check.
func withFID(raw layer2.RawBurst, dt enums.DataType, fid enums.FID) (layer2.RawBurst, bool) {
	p, ok := raw.MMDVMHostPayload(dt)
	if !ok {
		return raw, false
	}
	p[1] = byte(fid)
	if err := layer2.MMDVMHostSeal(dt, &p); err != nil {
		return raw, false
	}
	out := raw
	out.SetBPTCPayload(p)
	return out, true
}

// embeddedLC rewrites the embedded LC of voice bursts B to E for a copy of
// the call in another codec. A fragment that is the one of the call's LC
// becomes the fragment of the LC with the FID of the copy; any other
// fragment, such as one of a talker alias, stays as it is. An embedded voice
// LC that decodes and differs from the one known, or the first one without
// a Voice LC Header, is used from the next superframe on.
type embeddedLC struct {
	fid  enums.FID
	have bool
	lc   pdu.LCBytes
	next *pdu.LCBytes
	orig [4][4]byte
	cp   [4][4]byte
	frag [4][4]byte
	got  uint8
}

func (e *embeddedLC) setLC(lc pdu.LCBytes) {
	e.lc = lc
	e.next = nil
	e.orig = layer2.EncodeEmbeddedLC(lc)
	c := lc
	c.SetFID(e.fid)
	e.cp = layer2.EncodeEmbeddedLC(c)
	e.have = true
}

func (e *embeddedLC) rewrite(raw *layer2.RawBurst, pos uint) {
	if pos < 1 || pos > 4 {
		return
	}
	if pos == 1 && e.next != nil {
		e.setLC(*e.next)
	}
	d := raw.EmbeddedData()
	e.learn(d, pos)
	if e.have && d == e.orig[pos-1] {
		raw.SetEmbeddedData(e.cp[pos-1])
	}
}

func (e *embeddedLC) learn(d [4]byte, pos uint) {
	if pos == 1 {
		e.got = 0
	}
	e.frag[pos-1] = d
	e.got |= 1 << (pos - 1)
	if pos != 4 || e.got != 0xF {
		return
	}
	lc, res := layer2.DecodeEmbeddedLC(e.frag)
	if res.Uncorrectable {
		return
	}
	l := pdu.LCBytes(lc)
	if f := l.FLCO(); (f == enums.FLCOGroupVoiceChannelUser || f == enums.FLCOUnitToUnitVoiceChannelUser) && (!e.have || l != e.lc) {
		e.next = &l
	}
}
