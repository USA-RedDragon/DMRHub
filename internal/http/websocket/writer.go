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

package websocket

import (
	"context"

	"github.com/coder/websocket"
)

type Message struct {
	Type int
	Data []byte
}

// Writer sends messages to one websocket connection. WriteMessage never
// blocks: when the client falls bufferSize messages behind, the
// connection is dropped.
type Writer interface {
	WriteMessage(message Message)
}

type wsWriter struct {
	out    chan Message
	done   <-chan struct{}
	cancel context.CancelCauseFunc
}

func (w *wsWriter) WriteMessage(message Message) {
	select {
	case <-w.done:
		return
	default:
	}
	select {
	case w.out <- message:
	default:
		w.cancel(errStalled)
	}
}

// drain does not let the end of ctx cancel a write, because a cancelled
// write closes the TCP connection before closeConn can send a close frame.
// writeTimeout bounds each write instead.
func (w *wsWriter) drain(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-w.out:
			typ := websocket.MessageText
			if m.Type == int(websocket.MessageBinary) {
				typ = websocket.MessageBinary
			}
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
			err := conn.Write(writeCtx, typ, m.Data)
			cancel()
			if err != nil {
				w.cancel(errWriteFailed)
				return
			}
		}
	}
}
