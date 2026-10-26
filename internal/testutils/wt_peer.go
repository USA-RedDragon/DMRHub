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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

// WebTransportOn turns the WebTransport listener of the MMDVM server on, on
// a free port, for SetupIntegrationStack.
func WebTransportOn() Backend {
	return Backend{
		Name: "webtransport",
		Setup: func(_ *testing.T, cfg *config.Config) {
			cfg.DMR.MMDVM.WebTransport = config.MMDVMWebTransport{Enabled: true, Port: 0}
		},
	}
}

// moveRetry is how often a WebTransportPeer that was told to move tries
// another session, and moveTries how many times.
const (
	moveRetry = 100 * time.Millisecond
	moveTries = 50
	// moveOverlap is how long a WebTransportPeer keeps the old session
	// after it moved.
	moveOverlap = time.Second
	// dedupFor is how long a WebTransportPeer remembers the DMRD packets
	// that it got, so that it drops a copy that the other session brings.
	dedupFor = 2 * time.Second
)

// WebTransportPeer is a browser hotspot, as dmr-web is one: it logs in
// with the dmrgo session Peer over WebTransport datagrams, and when its
// replica drains, it opens a session to another replica, resumes its
// login there with the token of the drain notice, and moves to it without
// a gap.
type WebTransportPeer struct {
	ID uint32

	mu      sync.Mutex
	peer    *dmrdsession.Peer
	addr    string
	current *webtransport.Session
	start   time.Time
	downs   int
	ups     int
	moves   int
	seen    map[dmrKey]time.Time
	rxAt    time.Time
	heard   []Heard
	notices []time.Time
	moved   []time.Time

	ready    chan struct{}
	upOnce   sync.Once
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	packets  chan models.Packet
	closers  []func()
}

type dmrKey struct {
	slot   bool
	stream uint
	seq    uint
}

// NewWebTransportPeer returns a WebTransportPeer for the repeater id with
// the password.
func NewWebTransportPeer(id uint32, password string) (*WebTransportPeer, error) {
	var cfg dmrd.MMDVMConfig
	cfg.Reset()
	cfg.SetID(id)
	err := errors.Join(
		cfg.SetCallsign([]byte(fmt.Sprintf("W%04dWT", id%10000))),
		cfg.SetRXFrequency(440000000),
		cfg.SetTXFrequency(445000000),
		cfg.SetColorCode(1),
		cfg.SetLatitude([]byte("+30.2672")),
		cfg.SetLongitude([]byte("-097.7431")),
		cfg.SetLocation([]byte("Browser")),
		cfg.SetDescription([]byte("WebTransport peer")),
		cfg.SetSlots(3),
		cfg.SetSoftwareID([]byte("dmr-web")),
		cfg.SetPackageID([]byte("dmr-web")),
	)
	if err != nil {
		return nil, fmt.Errorf("building RPTC: %w", err)
	}
	peer, err := dmrdsession.NewPeer(&dmrdsession.PeerConfig{
		Dialect:  dmrd.DialectMMDVM,
		ID:       id,
		Password: []byte(password),
		Config:   cfg[:],
		Retry:    200 * time.Millisecond,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("making session peer: %w", err)
	}
	return &WebTransportPeer{
		ID:      id,
		peer:    peer,
		seen:    map[dmrKey]time.Time{},
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
		packets: make(chan models.Packet, clientBuffer),
	}, nil
}

func (p *WebTransportPeer) dial(ctx context.Context) (*webtransport.Session, error) {
	d := &webtransport.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a test client of a test hub
		QUICConfig:      &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rsp, sess, err := d.Dial(ctx, "https://"+p.addr+wtcert.Path, nil)
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("dialing WebTransport: %w", err)
	}
	if rsp.StatusCode != http.StatusOK {
		_ = d.Close()
		return nil, fmt.Errorf("the hub answered %d", rsp.StatusCode)
	}
	p.mu.Lock()
	p.closers = append(p.closers, func() { _ = sess.CloseWithError(0, ""); _ = d.Close() })
	p.mu.Unlock()
	return sess, nil
}

// Connect opens a session to the hub at addr and starts the login.
func (p *WebTransportPeer) Connect(addr string) error {
	p.addr = addr
	sess, err := p.dial(context.Background())
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.current, p.start = sess, time.Now()
	p.mu.Unlock()
	p.serve(sess)
	p.wg.Add(1)
	go p.ticks()
	return nil
}

// serve reads the datagrams and the drain notices of sess.
func (p *WebTransportPeer) serve(sess *webtransport.Session) {
	p.wg.Add(2)
	go func() {
		defer p.wg.Done()
		for {
			b, err := sess.ReceiveDatagram(sess.Context())
			if err != nil {
				return
			}
			p.packet(b)
		}
	}()
	go func() {
		defer p.wg.Done()
		for {
			str, err := sess.AcceptUniStream(sess.Context())
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(io.LimitReader(str, 1024)).ReadBytes('\n')
			var n struct {
				Drain  bool   `json:"drain"`
				Resume string `json:"resume"`
			}
			if json.Unmarshal(line, &n) == nil && n.Drain && n.Resume != "" {
				p.mu.Lock()
				p.notices = append(p.notices, time.Now())
				p.mu.Unlock()
				p.wg.Add(1)
				go p.move(sess, n.Resume)
			}
		}
	}()
}

// move opens sessions until one to a replica that does not drain takes the
// login over with token, then sends on it and closes old after
// moveOverlap. It reads each session from the start, as dmr-web does, since
// the replica sends the packets of the handover as soon as it takes the
// login over, and a session holds only a few datagrams that nobody reads.
func (p *WebTransportPeer) move(old *webtransport.Session, token string) {
	defer p.wg.Done()
	for range moveTries {
		select {
		case <-p.done:
			return
		default:
		}
		at := time.Now()
		sess, err := p.dial(context.Background())
		if err == nil {
			p.serve(sess)
		}
		if err == nil && p.resume(sess, token) {
			p.mu.Lock()
			p.current = sess
			p.moves++
			p.moved = append(p.moved, at)
			p.mu.Unlock()
			select {
			case <-p.done:
			case <-time.After(moveOverlap):
			}
			_ = old.CloseWithError(0, "moved")
			return
		}
		if sess != nil {
			_ = sess.CloseWithError(0, "")
		}
		time.Sleep(moveRetry)
	}
}

func (p *WebTransportPeer) resume(sess *webtransport.Session, token string) bool {
	ctx, cancel := context.WithTimeout(sess.Context(), 2*time.Second)
	defer cancel()
	str, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return false
	}
	defer func() { _ = str.Close() }()
	b, err := json.Marshal(map[string]string{"resume": token})
	if err != nil {
		return false
	}
	if _, err := str.Write(append(b, '\n')); err != nil {
		return false
	}
	line, err := bufio.NewReader(io.LimitReader(str, 1024)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	var r struct {
		Resumed bool `json:"resumed"`
	}
	return json.Unmarshal(line, &r) == nil && r.Resumed
}

func (p *WebTransportPeer) packet(b []byte) {
	at := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rxAt = at
	var out [dmrdsession.MaxOutputs]dmrdsession.Output
	p.run(p.peer.Packet(time.Since(p.start), b, out[:0]))
}

func (p *WebTransportPeer) ticks() {
	defer p.wg.Done()
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			p.mu.Lock()
			var out [dmrdsession.MaxOutputs]dmrdsession.Output
			p.run(p.peer.Tick(time.Since(p.start), out[:0]))
			p.mu.Unlock()
		}
	}
}

// run carries out the outputs of the Peer. The caller holds p.mu.
func (p *WebTransportPeer) run(out []dmrdsession.Output) {
	for _, o := range out {
		switch o.Kind {
		case dmrdsession.OutputSend:
			if p.current != nil {
				_ = p.current.SendDatagram(append([]byte(nil), o.Packet...))
			}
		case dmrdsession.OutputUp:
			p.ups++
			p.upOnce.Do(func() { close(p.ready) })
		case dmrdsession.OutputDown:
			p.downs++
		case dmrdsession.OutputData:
			if o.Command != dmrd.CommandDMRD {
				continue
			}
			d, err := dmrd.DecodeData(o.Packet)
			if err != nil {
				continue
			}
			pkt := models.PacketFromData(&d, d.Size > 53)
			if len(p.heard) < maxHeard {
				p.heard = append(p.heard, Heard{At: p.rxAt, Packet: pkt})
			}
			k := dmrKey{slot: pkt.Slot, stream: pkt.StreamID, seq: pkt.Seq}
			now := time.Now()
			if at, dup := p.seen[k]; dup && now.Sub(at) < dedupFor {
				continue
			}
			p.seen[k] = now
			for key, at := range p.seen {
				if now.Sub(at) > dedupFor {
					delete(p.seen, key)
				}
			}
			select {
			case p.packets <- pkt:
			default:
			}
		case dmrdsession.OutputLogin, dmrdsession.OutputConfig, dmrdsession.OutputOptions, dmrdsession.OutputMessage, dmrdsession.OutputPong:
		}
	}
}

// WaitReady blocks until the login is complete or the timeout expires.
func (p *WebTransportPeer) WaitReady(timeout time.Duration) error {
	select {
	case <-p.ready:
		return nil
	case <-time.After(timeout):
		return ErrSessionPeerTimeout
	}
}

// Downs returns how many times the session ended.
func (p *WebTransportPeer) Downs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.downs
}

// Ups returns how many times the login completed.
func (p *WebTransportPeer) Ups() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ups
}

// Moves returns how many times the peer moved to another session.
func (p *WebTransportPeer) Moves() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.moves
}

// Heard returns the DMRD packets that the peer got on any of its sessions,
// copies too, each with the time it came, in the order they came.
func (p *WebTransportPeer) Heard() []Heard {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Heard(nil), p.heard...)
}

// Notices returns when the drain notices came, oldest first.
func (p *WebTransportPeer) Notices() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.notices...)
}

// Moved returns, for each move to another session, when the peer began to
// open that session, oldest first.
func (p *WebTransportPeer) Moved() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.moved...)
}

// SendDMRD sends a DMRD packet with the repeater ID of the peer on the
// current session.
func (p *WebTransportPeer) SendDMRD(pkt models.Packet) error {
	pkt.Repeater = uint(p.ID)
	p.mu.Lock()
	sess := p.current
	p.mu.Unlock()
	if err := sess.SendDatagram(pkt.Encode()); err != nil {
		return fmt.Errorf("sending a DMRD datagram: %w", err)
	}
	return nil
}

// Drain collects the DMRD packets received within timeout.
func (p *WebTransportPeer) Drain(timeout time.Duration) []models.Packet {
	var result []models.Packet
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case pkt := <-p.packets:
			result = append(result, pkt)
		case <-timer.C:
			return result
		}
	}
}

// Close ends the sessions.
func (p *WebTransportPeer) Close() {
	p.stopOnce.Do(func() {
		close(p.done)
		p.mu.Lock()
		closers := p.closers
		p.mu.Unlock()
		for _, c := range closers {
			c()
		}
	})
	p.wg.Wait()
}
