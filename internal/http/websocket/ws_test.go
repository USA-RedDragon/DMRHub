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

package websocket_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/stretchr/testify/assert"
)

func TestMessageStruct(t *testing.T) {
	t.Parallel()
	msg := websocket.Message{
		Type: 1,
		Data: []byte("hello"),
	}
	assert.Equal(t, 1, msg.Type)
	assert.Equal(t, []byte("hello"), msg.Data)
}

func TestMessageEmptyData(t *testing.T) {
	t.Parallel()
	msg := websocket.Message{
		Type: 2,
		Data: nil,
	}
	assert.Equal(t, 2, msg.Type)
	assert.Nil(t, msg.Data)
}

func TestMessageBinaryData(t *testing.T) {
	t.Parallel()
	data := []byte{0x00, 0x01, 0x02, 0xFF}
	msg := websocket.Message{
		Type: 2,
		Data: data,
	}
	assert.Equal(t, 2, msg.Type)
	assert.Equal(t, data, msg.Data)
	assert.Len(t, msg.Data, 4)
}
