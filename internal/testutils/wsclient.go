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

package testutils

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	wsOpText  = 0x1
	wsOpClose = 0x8
	wsOpPing  = 0x9
	wsOpPong  = 0xA
	wsFin     = 0x80
	wsMask    = 0x80
)

// ErrWSClosed is returned by WSConn.ReadText when the server closed the
// connection.
var ErrWSClosed = errors.New("websocket closed")

// WSCloseError is the close frame that the server sent. It matches
// ErrWSClosed with errors.Is.
type WSCloseError struct {
	Code   int
	Reason string
}

func (e *WSCloseError) Error() string {
	return fmt.Sprintf("websocket closed with %d %q", e.Code, e.Reason)
}

// Is reports whether target is ErrWSClosed.
func (e *WSCloseError) Is(target error) bool {
	return target == ErrWSClosed
}

// WSConn is a small RFC 6455 client for tests. It needs no websocket
// library, so the tests do not depend on the one the server uses. Like a
// browser, it answers the pings of the server whether or not the test is
// reading.
type WSConn struct {
	conn      net.Conn
	r         *bufio.Reader
	writeMu   sync.Mutex
	frames    chan wsFrame
	ended     chan struct{}
	end       error
	closed    chan struct{}
	closeOnce sync.Once
}

type wsFrame struct {
	text string
	err  error
}

// DialWS opens the websocket path on the test server at serverURL with the
// cookies of jar. It returns the handshake response; conn is nil when the
// server did not switch protocols.
func DialWS(t *testing.T, serverURL, path string, jar CookieJar) (*WSConn, *http.Response) {
	t.Helper()
	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	return DialWSFrom(t, serverURL, path, jar, "http://"+u.Host)
}

// DialWSFrom opens the websocket path as DialWS does, from a page at
// origin.
func DialWSFrom(t *testing.T, serverURL, path string, jar CookieJar, origin string) (*WSConn, *http.Response) {
	t.Helper()
	return dialWS(t, serverURL, path, jar, origin, nil)
}

// DialWSProtocols opens the websocket path from a page at origin, without
// cookies, offering the subprotocols protocols.
func DialWSProtocols(t *testing.T, serverURL, path, origin string, protocols ...string) (*WSConn, *http.Response) {
	t.Helper()
	return dialWS(t, serverURL, path, CookieJar{}, origin, protocols)
}

func dialWS(t *testing.T, serverURL, path string, jar CookieJar, origin string, protocols []string) (*WSConn, *http.Response) {
	t.Helper()
	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(t.Context(), "tcp", u.Host)
	require.NoError(t, err)
	var key [16]byte
	_, err = rand.Read(key[:])
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+u.Host+path, nil)
	require.NoError(t, err)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(key[:]))
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Origin", origin)
	if len(protocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(protocols, ", "))
	}
	for _, c := range jar.Cookies() {
		req.Header.Add("Cookie", c.String())
	}
	require.NoError(t, req.Write(conn))
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, req)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, resp
	}
	w := &WSConn{
		conn:   conn,
		r:      r,
		frames: make(chan wsFrame, 64),
		ended:  make(chan struct{}),
		closed: make(chan struct{}),
	}
	go w.readLoop()
	t.Cleanup(w.Close)
	return w, resp
}

// Close closes the connection.
func (w *WSConn) Close() {
	w.closeOnce.Do(func() {
		close(w.closed)
		_ = w.conn.Close()
	})
}

func (w *WSConn) write(op byte, payload []byte) error {
	frame := []byte{wsFin | op}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, wsMask|byte(n))
	case n <= 0xffff:
		frame = append(frame, wsMask|126, byte(n>>8), byte(n))
	default:
		frame = append(frame, wsMask|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("mask: %w", err)
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if _, err := w.conn.Write(frame); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// WriteText sends s as a text frame.
func (w *WSConn) WriteText(t *testing.T, s string) {
	t.Helper()
	require.NoError(t, w.write(wsOpText, []byte(s)))
}

// ReadText returns the next text frame. It returns an error that wraps
// os.ErrDeadlineExceeded when none comes within timeout, a *WSCloseError
// once the server closed the connection, and the read error once the
// connection broke.
func (w *WSConn) ReadText(timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case f := <-w.frames:
		return f.text, f.err
	case <-w.ended:
		select {
		case f := <-w.frames:
			return f.text, f.err
		default:
			return "", w.end
		}
	case <-timer.C:
		return "", fmt.Errorf("read: %w", os.ErrDeadlineExceeded)
	}
}

func (w *WSConn) readLoop() {
	var end error
	defer func() {
		w.end = end
		close(w.ended)
	}()
	for {
		text, isText, err := w.readFrame()
		if err == nil && !isText {
			continue
		}
		select {
		case w.frames <- wsFrame{text: text, err: err}:
		case <-w.closed:
			end = net.ErrClosed
			return
		}
		if err != nil {
			end = err
			return
		}
	}
}

// readFrame reads the next frame and answers it when it is a ping. It
// returns the text of a text frame with isText set, and an error once the
// server closed the connection or it broke.
func (w *WSConn) readFrame() (text string, isText bool, err error) {
	var head [2]byte
	if _, err := io.ReadFull(w.r, head[:]); err != nil {
		return "", false, fmt.Errorf("read: %w", err)
	}
	n := uint64(head[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(w.r, ext[:]); err != nil {
			return "", false, fmt.Errorf("read: %w", err)
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(w.r, ext[:]); err != nil {
			return "", false, fmt.Errorf("read: %w", err)
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	masked := head[1]&wsMask != 0
	if masked {
		if _, err := io.ReadFull(w.r, mask[:]); err != nil {
			return "", false, fmt.Errorf("read: %w", err)
		}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(w.r, payload); err != nil {
		return "", false, fmt.Errorf("read: %w", err)
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	switch head[0] & 0x0f {
	case wsOpText:
		return string(payload), true, nil
	case wsOpPing:
		if err := w.write(wsOpPong, payload); err != nil {
			return "", false, err
		}
	case wsOpClose:
		_ = w.write(wsOpClose, payload)
		closeErr := &WSCloseError{}
		if len(payload) >= 2 {
			closeErr.Code = int(binary.BigEndian.Uint16(payload[:2]))
			closeErr.Reason = string(payload[2:])
		}
		return "", false, closeErr
	}
	return "", false, nil
}
