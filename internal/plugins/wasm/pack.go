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

package wasm

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
)

const (
	sectionData      = 11
	sectionDataCount = 12
	opI32Const       = 0x41
	opEnd            = 0x0b
	// packGap is the longest run of zeros that packData writes to join two
	// data segments. Each segment costs the runtime about 100 bytes.
	packGap = 128
)

var errNotPackable = errors.New("the data segments cannot be packed")

type segment struct {
	offset uint32
	data   []byte
}

// packData returns wasm with its data segments joined where they are at
// most packGap bytes apart, with zeros between them. The Go linker writes
// one segment for each run of bytes that are not zero, tens of thousands,
// and the runtime keeps each one while the plugin runs. Memory starts as
// zeros and no two segments overlap, so the joined segments write the same
// memory. packData returns wasm as it is when it cannot tell that, such as
// for passive segments or segments that overlap.
func packData(wasm []byte) []byte {
	if len(wasm) < 8 {
		return wasm
	}
	pos := 8
	start, end := -1, -1
	for pos < len(wasm) {
		id := wasm[pos]
		size, n, ok := readU32(wasm[pos+1:])
		if !ok || uint64(pos)+1+uint64(n)+uint64(size) > uint64(len(wasm)) { //nolint:gosec // pos and n are not negative
			return wasm
		}
		body := pos + 1 + n
		switch id {
		case sectionDataCount:
			return wasm
		case sectionData:
			start, end = pos, body+int(size)
		}
		pos = body + int(size)
	}
	if start < 0 {
		return wasm
	}
	segs, err := readSegments(wasm[start+1 : end])
	if err != nil {
		return wasm
	}
	packed := joinSegments(segs)
	if len(packed) == len(segs) {
		return wasm
	}
	section := writeSegments(packed)
	out := make([]byte, 0, len(wasm)-(end-start)+len(section)+1+binary.MaxVarintLen32)
	out = append(out, wasm[:start]...)
	out = append(out, sectionData)
	out = binary.AppendUvarint(out, uint64(len(section)))
	out = append(out, section...)
	return append(out, wasm[end:]...)
}

func readSegments(b []byte) ([]segment, error) {
	_, n, ok := readU32(b)
	if !ok {
		return nil, errNotPackable
	}
	b = b[n:]
	count, n, ok := readU32(b)
	if !ok {
		return nil, errNotPackable
	}
	b = b[n:]
	segs := make([]segment, 0, count)
	for range count {
		flag, n, ok := readU32(b)
		if !ok || flag != 0 || len(b) < n+1 || b[n] != opI32Const {
			return nil, errNotPackable
		}
		b = b[n+1:]
		offset, n, ok := readS32(b)
		if !ok || len(b) < n+1 || b[n] != opEnd {
			return nil, errNotPackable
		}
		b = b[n+1:]
		size, n, ok := readU32(b)
		if !ok || uint64(len(b)) < uint64(n)+uint64(size) { //nolint:gosec // n is not negative
			return nil, errNotPackable
		}
		segs = append(segs, segment{offset: uint32(offset), data: b[n : n+int(size)]}) //nolint:gosec // an i32.const is an address of 32 bits
		b = b[n+int(size):]
	}
	if len(b) != 0 {
		return nil, errNotPackable
	}
	slices.SortStableFunc(segs, func(a, b segment) int { return cmp.Compare(a.offset, b.offset) })
	for i := 1; i < len(segs); i++ {
		if uint64(segs[i].offset) < uint64(segs[i-1].offset)+uint64(len(segs[i-1].data)) {
			return nil, errNotPackable
		}
	}
	return segs, nil
}

func joinSegments(segs []segment) []segment {
	var out []segment
	for i := 0; i < len(segs); {
		j := i + 1
		end := uint64(segs[i].offset) + uint64(len(segs[i].data))
		for j < len(segs) && uint64(segs[j].offset)-end <= packGap {
			end = uint64(segs[j].offset) + uint64(len(segs[j].data))
			j++
		}
		if j == i+1 {
			out = append(out, segs[i])
			i = j
			continue
		}
		data := make([]byte, end-uint64(segs[i].offset))
		for _, s := range segs[i:j] {
			copy(data[s.offset-segs[i].offset:], s.data)
		}
		out = append(out, segment{offset: segs[i].offset, data: data})
		i = j
	}
	return out
}

func writeSegments(segs []segment) []byte {
	size := binary.MaxVarintLen32
	for _, s := range segs {
		size += 3 + 2*binary.MaxVarintLen32 + len(s.data)
	}
	out := make([]byte, 0, size)
	out = binary.AppendUvarint(out, uint64(len(segs)))
	for _, s := range segs {
		out = append(out, 0, opI32Const)
		out = appendS32(out, int32(s.offset)) //nolint:gosec // an i32.const holds the address as signed
		out = append(out, opEnd)
		out = binary.AppendUvarint(out, uint64(len(s.data)))
		out = append(out, s.data...)
	}
	return out
}

func appendS32(b []byte, v int32) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && c&0x40 == 0) || (v == -1 && c&0x40 != 0) {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

func readU32(b []byte) (uint32, int, bool) {
	v, n := binary.Uvarint(b)
	if n <= 0 || n > binary.MaxVarintLen32 || v > 1<<32-1 {
		return 0, 0, false
	}
	return uint32(v), n, true
}

func readS32(b []byte) (int32, int, bool) {
	var v int64
	var shift uint
	for i := 0; i < len(b) && i < binary.MaxVarintLen32; i++ {
		c := b[i]
		v |= int64(c&0x7f) << shift
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				v |= -1 << shift
			}
			if v < -1<<31 || v > 1<<31-1 {
				return 0, 0, false
			}
			return int32(v), i + 1, true
		}
	}
	return 0, 0, false
}
