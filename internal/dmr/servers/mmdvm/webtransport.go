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

package mmdvm

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

const (
	webTransportName = "webtransport"
	// WebTransportPath is the path of the WebTransport endpoint of the
	// MMDVM server.
	WebTransportPath = "/mmdvm"
)

var errNoSession = errors.New("no WebTransport session at the address")

// wtAddr is the address of a WebTransport session.
type wtAddr string

func (a wtAddr) Network() string { return webTransportName }
func (a wtAddr) String() string  { return string(a) }

type wtDatagram struct {
	data []byte
	from wtAddr
}

// webTransport is the transport of browser hotspots and radios: each
// WebTransport session is a peer, and each datagram is one packet of the
// MMDVM network protocol.
type webTransport struct {
	udp      net.PacketConn
	srv      *webtransport.Server
	sessions *xsync.Map[string, *webtransport.Session]
	in       chan wtDatagram
	done     chan struct{}
	once     sync.Once
}

func listenWebTransport(bind string, port int, certs *wtcert.Source) (*webTransport, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(bind), Port: port})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenSocket, err)
	}
	t := &webTransport{
		udp:      udp,
		sessions: xsync.NewMap[string, *webtransport.Session](),
		in:       make(chan wtDatagram, channelBufferSize),
		done:     make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(WebTransportPath, t.accept)
	h3 := &http3.Server{
		Handler:    mux,
		TLSConfig:  http3.ConfigureTLSConfig(&tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS13}),
		QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
	}
	webtransport.ConfigureHTTP3Server(h3)
	t.srv = &webtransport.Server{H3: h3, CheckOrigin: func(*http.Request) bool { return true }}
	go func() {
		if err := t.srv.Serve(udp); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
			select {
			case <-t.done:
			default:
				slog.Error("WebTransport listener stopped", "error", err)
			}
		}
	}()
	return t, nil
}

// Addr returns the address of the UDP socket of the listener.
func (t *webTransport) Addr() net.Addr {
	return t.udp.LocalAddr()
}

func (t *webTransport) accept(w http.ResponseWriter, r *http.Request) {
	sess, err := t.srv.Upgrade(w, r)
	if err != nil {
		slog.Debug("WebTransport session not accepted", "remoteAddr", r.RemoteAddr, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	addr := wtAddr(hex.EncodeToString(id[:]))
	t.sessions.Store(string(addr), sess)
	slog.Debug("WebTransport session opened", "session", addr, "remoteAddr", r.RemoteAddr)
	go t.read(sess.Context(), addr, sess) //nolint:contextcheck // the request context ends when this handler returns, the session lives on
}

func (t *webTransport) read(ctx context.Context, addr wtAddr, sess *webtransport.Session) {
	defer t.sessions.Delete(string(addr))
	for {
		b, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			slog.Debug("WebTransport session ended", "session", addr, "error", err)
			return
		}
		select {
		case t.in <- wtDatagram{data: b, from: addr}:
		case <-t.done:
			return
		}
	}
}

func (t *webTransport) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case d := <-t.in:
		return copy(p, d.data), d.from, nil
	case <-t.done:
		return 0, nil, net.ErrClosed
	}
}

func (t *webTransport) WriteTo(p []byte, addr net.Addr) (int, error) {
	sess, ok := t.sessions.Load(addr.String())
	if !ok {
		return 0, errNoSession
	}
	if err := sess.SendDatagram(append([]byte(nil), p...)); err != nil {
		return 0, fmt.Errorf("sending a WebTransport datagram: %w", err)
	}
	return len(p), nil
}

func (t *webTransport) Close() error {
	var err error
	t.once.Do(func() {
		close(t.done)
		t.sessions.Range(func(_ string, sess *webtransport.Session) bool {
			_ = sess.CloseWithError(0, "server stopping")
			return true
		})
		err = errors.Join(t.srv.Close(), t.udp.Close())
	})
	return err
}

var _ transport = (*webTransport)(nil)
