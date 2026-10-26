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
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
)

// ProbeInterval is how often a Service asks each AMBE server for its
// product name, so that its status shows whether it answers between calls.
// It does not ask a server while calls hold it, so that a server that
// answers only its last client does not send their answers to the probe.
const ProbeInterval = 5 * time.Second

// probeTimeout is how long a probe waits for the answer.
const probeTimeout = time.Second

// Errors of a Service.
var (
	// ErrOff is returned by NewStream when no AMBE server is set.
	ErrOff = errors.New("transcode: no AMBE server is set")
	// ErrCodecs is returned by NewStream for a pair of codecs that is not
	// AMBE and Codec2.
	ErrCodecs = errors.New("transcode: can convert only between AMBE and Codec2")
	// ErrBusy is returned when every AMBE server holds as many calls as it
	// takes.
	ErrBusy = errors.New("transcode: every AMBE server is busy")
)

// ServerConfig is one AMBE server: its host and UDP port, and the calls it
// converts at once, 0 for no limit. An AMBEserver with one DVSI chip takes
// 1, as it answers only its last client.
type ServerConfig struct {
	Addr  string
	Calls int
}

// ServerStatus is the state of one AMBE server.
type ServerStatus struct {
	ServerConfig
	// AnsweredAt is when the server last answered, a probe or a frame, and
	// Answered whether it did since it was set.
	AnsweredAt time.Time
	Answered   bool
	// Calls is the number of calls that hold the server now, on every
	// replica that shares the KV store.
	CallsNow int
}

// server is one AMBE server and when it last answered.
type server struct {
	ServerConfig
	answered atomic.Int64
	cancel   context.CancelFunc
	leases   *leases
	// formatRefused reports that the server refused PKT_SPCHFMT, so that
	// the hub logs it once until the server takes it.
	formatRefused atomic.Bool
}

func (s *server) answer() {
	s.answered.Store(time.Now().UnixNano())
}

// Service converts calls with the AMBE servers that SetServers sets. Each
// call holds one server for its whole length; the KV store shares the
// calls that hold each server between the replicas. It is safe for
// concurrent use.
type Service struct {
	mu      sync.Mutex
	kv      kv.KV
	timing  atomic.Pointer[timing]
	servers atomic.Pointer[[]*server]
	closed  bool
	wg      sync.WaitGroup
	refused atomic.Uint64
}

type timing struct {
	budget, attempt time.Duration
	now             func() time.Time
}

// NewService returns a Service without an AMBE server that shares the
// calls that hold each server in store. A nil store counts the calls of
// this process only. Its calls use Budget and AttemptTimeout.
func NewService(store kv.KV) *Service {
	s := &Service{kv: store}
	s.timing.Store(&timing{budget: Budget, attempt: AttemptTimeout, now: time.Now})
	return s
}

// SetTiming sets the time each burst of the calls that start later may take
// to convert, and how long each request to an AMBE server waits for its
// answer. A test that checks what a conversion makes, and not when, gives
// them room on a slow machine.
func (s *Service) SetTiming(budget, attempt time.Duration) {
	s.timing.Store(&timing{budget: budget, attempt: attempt, now: s.timing.Load().now})
}

// SetServers sets the AMBE servers, in the order a call tries them. An
// empty list turns transcoding off. Calls that are being converted keep the
// server they hold.
func (s *Service) SetServers(list []ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	var old []*server
	if p := s.servers.Load(); p != nil {
		old = *p
	}
	next := make([]*server, 0, len(list))
	for _, c := range list {
		if c.Calls < 0 {
			c.Calls = 0
		}
		i := slices.IndexFunc(old, func(o *server) bool { return o != nil && o.ServerConfig == c })
		if i >= 0 {
			next = append(next, old[i])
			old[i] = nil
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		srv := &server{ServerConfig: c, cancel: cancel, leases: newLeases(s.kv, c)}
		next = append(next, srv)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			probe(ctx, srv)
		}()
	}
	for _, o := range old {
		if o != nil {
			o.cancel()
		}
	}
	if len(next) == 0 {
		s.servers.Store(nil)
		return
	}
	s.servers.Store(&next)
	go warmCodec2()
}

func (s *Service) list() []*server {
	if p := s.servers.Load(); p != nil {
		return *p
	}
	return nil
}

// Enabled reports whether an AMBE server is set.
func (s *Service) Enabled() bool {
	return len(s.list()) > 0
}

// Refused returns the number of calls since NewService whose first burst
// found every AMBE server busy, on this replica.
func (s *Service) Refused() uint64 {
	return s.refused.Load()
}

// Status returns the state of each AMBE server, in order.
func (s *Service) Status(ctx context.Context) []ServerStatus {
	list := s.list()
	out := make([]ServerStatus, 0, len(list))
	for _, srv := range list {
		st := ServerStatus{ServerConfig: srv.ServerConfig, CallsNow: srv.leases.count(ctx)}
		if n := srv.answered.Load(); n != 0 {
			st.AnsweredAt, st.Answered = time.Unix(0, n), true
		}
		out = append(out, st)
	}
	return out
}

// Close stops the probes. Streams that are open keep working until they
// are closed.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, srv := range s.list() {
		srv.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// NewStream returns a Stream that converts one call from the codec from to
// the codec to. mode is the Codec2 mode of the output to Codec2;
// codec2.ModeAuto selects Mode2400. It returns ErrOff without an AMBE
// server, ErrCodecs for codecs other than AMBE and Codec2 in either order,
// and the errors of codec2.New. It contacts no AMBE server and takes none;
// the first burst takes one.
func (s *Service) NewStream(from, to Codec, mode codec2.Mode) (*Stream, error) {
	list := s.list()
	if len(list) == 0 {
		return nil, ErrOff
	}
	if from.Other() != to || to == CodecUnknown {
		return nil, ErrCodecs
	}
	cfg := &codec2.Config{}
	if to == CodecCodec2 {
		cfg.Mode = codec2.Mode2400
		if mode == codec2.Mode3200 {
			cfg.Mode = codec2.Mode3200
		}
	}
	c2, err := codec2.New(cfg)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	tm := s.timing.Load()
	return &Stream{servers: list, from: from, to: to, c2: c2, emb: embeddedLC{fid: FIDOf(to)}, pcm: make([]int16, 0, dvsi.FrameSamples), budget: tm.budget, attempt: tm.attempt, now: tm.now, refused: &s.refused}, nil
}

func warmCodec2() {
	c, err := codec2.New(nil)
	if err != nil {
		slog.Error("Codec2 is not available, calls cannot be converted", "error", err)
		return
	}
	_ = c.Close()
}

func probe(ctx context.Context, srv *server) {
	t := time.NewTicker(ProbeInterval)
	defer t.Stop()
	var conn net.Conn
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	for {
		if time.Since(time.Unix(0, srv.answered.Load())) >= ProbeInterval && srv.leases.count(ctx) == 0 {
			if conn == nil {
				var d net.Dialer
				if c, err := d.DialContext(ctx, "udp", srv.Addr); err == nil {
					conn = c
				}
			}
			if conn != nil {
				ok, broken := askProduct(conn)
				if ok {
					srv.answer()
				}
				if broken {
					_ = conn.Close()
					conn = nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// askProduct sends a product query on conn and waits probeTimeout for the
// answer. broken reports that conn cannot send and must be opened again.
func askProduct(conn net.Conn) (ok, broken bool) {
	_ = conn.SetDeadline(time.Now().Add(probeTimeout))
	if _, err := conn.Write(dvsi.AppendQuery(nil, true)); err != nil {
		return false, true
	}
	buf := make([]byte, dvsi.MaxPacketLen)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return false, false
		}
		for _, parity := range []bool{true, false} {
			p, _, err := dvsi.Parse(buf[:n], parity)
			if err != nil {
				continue
			}
			if _, ok := p.Response(dvsi.FieldProdID); ok {
				return true, false
			}
		}
	}
}
