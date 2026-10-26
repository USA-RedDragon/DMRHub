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
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

const webTransportName = "webtransport"

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
	udp      *wtConn
	tr       *quic.Transport
	ln       *quic.EarlyListener
	srv      *webtransport.Server
	sessions *xsync.Map[string, *webtransport.Session]
	in       chan wtDatagram
	done     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup

	// draining refuses new sessions, so that browsers that move go to
	// another replica.
	draining atomic.Bool
	// onResume takes the resume request of a session.
	onResume atomic.Pointer[func(wtAddr, string) wtResume]
	// onPathMoved takes each session whose QUIC connection the load
	// balancer moved to another path, and the time of it.
	onPathMoved atomic.Pointer[func(wtAddr, time.Time)]
}

// controlWait is the longest time the listener waits to open or read a
// control stream.
const controlWait = 5 * time.Second

// maxControl is the longest control message, in octets.
const maxControl = 1024

// listenWebTransport listens for WebTransport sessions on the UDP port.
// The replicas share secret, from which they make one stateless reset key,
// and forward the packets of each other's connections over ps; with a nil
// ps or store the listener works alone.
func listenWebTransport(bind string, port int, certs *wtcert.Source, secret []byte, ps pubsub.PubSub, store kv.KV) (*webTransport, error) {
	sock, _, err := servers.ListenUDP(bind, port)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenSocket, err)
	}
	tag, err := newReplicaTag()
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	udp := newWTConn(sock, tag, ps, store)
	reset := quic.StatelessResetKey(sha256.Sum256(append([]byte("dmrhub webtransport stateless reset"), secret...)))
	tr := &quic.Transport{Conn: udp, ConnectionIDGenerator: cidGenerator{tag: tag}, StatelessResetKey: &reset}
	t := &webTransport{
		udp:      udp,
		tr:       tr,
		sessions: xsync.NewMap[string, *webtransport.Session](),
		in:       make(chan wtDatagram, channelBufferSize),
		done:     make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(wtcert.Path, t.accept)
	h3 := &http3.Server{
		Handler:    mux,
		TLSConfig:  http3.ConfigureTLSConfig(&tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS13}),
		QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true, InitialPacketSize: 1200},
	}
	webtransport.ConfigureHTTP3Server(h3)
	t.srv = &webtransport.Server{H3: h3, CheckOrigin: func(*http.Request) bool { return true }}
	moved := t.pathMoved
	udp.onPath.Store(&moved)
	ln, err := tr.ListenEarly(h3.TLSConfig, h3.QUICConfig)
	if err != nil {
		_ = udp.Close()
		return nil, fmt.Errorf("%w: %w", ErrOpenSocket, err)
	}
	t.ln = ln
	t.wg.Add(1)
	go t.serve()
	return t, nil
}

// serve hands each QUIC connection to the WebTransport server. It accepts
// the connections itself, rather than through webtransport.Server.Serve,
// whose wait group races with a Close right after the start.
func (t *webTransport) serve() {
	defer t.wg.Done()
	for {
		conn, err := t.ln.Accept(context.Background())
		if err != nil {
			select {
			case <-t.done:
			default:
				slog.Error("WebTransport listener stopped", "error", err)
			}
			return
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.srv.ServeQUICConn(conn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Debug("WebTransport connection ended", "error", err)
			}
		}()
	}
}

// pathMoved hands each session on the QUIC connection that the stack
// knows by addr, whose packets began to come another way at at, to
// onPathMoved.
func (t *webTransport) pathMoved(addr net.Addr, at time.Time) {
	fn := t.onPathMoved.Load()
	if fn == nil {
		return
	}
	t.sessions.Range(func(key string, sess *webtransport.Session) bool {
		if sess.RemoteAddr().String() == addr.String() {
			(*fn)(wtAddr(key), at)
		}
		return true
	})
}

// Addr returns the address of the UDP socket of the listener.
func (t *webTransport) Addr() net.Addr {
	return t.udp.LocalAddr()
}

func (t *webTransport) accept(w http.ResponseWriter, r *http.Request) {
	if t.draining.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
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
	go t.read(sess.Context(), addr, sess)    //nolint:contextcheck // the request context ends when this handler returns, the session lives on
	go t.control(sess.Context(), addr, sess) //nolint:contextcheck // the request context ends when this handler returns, the session lives on
}

// control answers the resume requests that the browser sends on streams
// of the session.
func (t *webTransport) control(ctx context.Context, addr wtAddr, sess *webtransport.Session) {
	for {
		str, err := sess.AcceptStream(ctx)
		if err != nil {
			return
		}
		go t.answer(addr, str)
	}
}

func (t *webTransport) answer(addr wtAddr, str *webtransport.Stream) {
	defer func() { _ = str.Close() }()
	_ = str.SetDeadline(time.Now().Add(controlWait))
	line, err := bufio.NewReader(io.LimitReader(str, maxControl)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	var req wtResume
	reply := wtResume{Reason: "bad request"}
	if json.Unmarshal(line, &req) == nil && req.Resume != "" {
		if fn := t.onResume.Load(); fn != nil {
			reply = (*fn)(addr, req.Resume)
		}
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_, _ = str.Write(append(b, '\n'))
}

// notify sends msg on a new stream to the session at addr.
func (t *webTransport) notify(ctx context.Context, addr wtAddr, msg any) error {
	sess, ok := t.sessions.Load(string(addr))
	if !ok {
		return errNoSession
	}
	ctx, cancel := context.WithTimeout(ctx, controlWait)
	defer cancel()
	str, err := sess.OpenUniStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("opening a WebTransport stream: %w", err)
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encoding a WebTransport message: %w", err)
	}
	_ = str.SetWriteDeadline(time.Now().Add(controlWait))
	if _, err := str.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("sending a WebTransport message: %w", err)
	}
	if err := str.Close(); err != nil {
		return fmt.Errorf("closing a WebTransport stream: %w", err)
	}
	return nil
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

// unregister removes the tag of the replica, so that the others reset
// its connections at once instead of forwarding to it.
func (t *webTransport) unregister(ctx context.Context) {
	t.udp.unregister(ctx)
}

func (t *webTransport) Close() error {
	var err error
	t.once.Do(func() {
		close(t.done)
		t.sessions.Range(func(_ string, sess *webtransport.Session) bool {
			_ = sess.CloseWithError(0, "server stopping")
			return true
		})
		err = errors.Join(t.srv.Close(), t.ln.Close(), t.tr.Close(), t.udp.Close())
		t.wg.Wait()
	})
	return err
}

var _ transport = (*webTransport)(nil)
