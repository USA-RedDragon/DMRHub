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

package hub

import "sync"

// marshalBufSize is large enough for a MarshalMsg'd RawDMRPacket containing an encoded Packet.
const marshalBufSize = 128

// broadcastBufSize is large enough for [1-byte name len][name (max 255)][encoded packet (55)].
const broadcastBufSize = 1 + 255 + 55

var marshalPool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		b := make([]byte, marshalBufSize)
		return &b
	},
}

var broadcastPool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		b := make([]byte, broadcastBufSize)
		return &b
	},
}

func (h *Hub) getMarshalBuffer() *[]byte {
	return marshalPool.Get().(*[]byte) //nolint:errcheck,forcetypeassert
}

func (h *Hub) putMarshalBuffer(b *[]byte) {
	marshalPool.Put(b)
}

func (h *Hub) getBroadcastBuffer(minSize int) *[]byte {
	bp := broadcastPool.Get().(*[]byte) //nolint:errcheck,forcetypeassert
	if cap(*bp) < minSize {
		*bp = make([]byte, minSize)
	} else {
		*bp = (*bp)[:cap(*bp)]
	}
	return bp
}

func (h *Hub) putBroadcastBuffer(b *[]byte) {
	broadcastPool.Put(b)
}
