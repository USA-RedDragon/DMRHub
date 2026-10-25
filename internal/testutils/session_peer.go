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
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
)

// ErrSessionPeerTimeout is the error of SessionPeer.WaitReady when the
// login does not complete in time.
var ErrSessionPeerTimeout = errors.New("session peer login timed out")

// SessionPeer is a repeater that logs in with the dmrgo session Peer: in
// the MMDVM network protocol as DMRGateway does, with RPTO options when it
// has them, or in HBRP.
type SessionPeer struct {
	ID      uint32
	Dialect dmrd.Dialect

	mu      sync.Mutex
	peer    *dmrdsession.Peer
	conn    *net.UDPConn
	target  *net.UDPAddr
	start   time.Time
	downs   int
	ups     int
	noTicks bool

	ready    chan struct{}
	upOnce   sync.Once
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	packets  chan models.Packet
}

// NewSessionPeer returns a SessionPeer for the repeater id with the
// password and the RPTO options, which may be empty. It pings every 200 ms
// and logs in again after 5 s without an answer.
func NewSessionPeer(id uint32, password, options string) (*SessionPeer, error) {
	return NewSessionPeerTimed(id, password, options, 200*time.Millisecond, 5*time.Second)
}

// NewSessionPeerTimed is NewSessionPeer with the time between two pings, or
// two sends of a login step, and the time without an answer after which it
// logs in again, as MMDVMHost pings every 10 s.
func NewSessionPeerTimed(id uint32, password, options string, retry, timeout time.Duration) (*SessionPeer, error) {
	var cfg dmrd.MMDVMConfig
	cfg.Reset()
	cfg.SetID(id)
	err := errors.Join(
		cfg.SetCallsign([]byte(fmt.Sprintf("S%04dST", id%10000))),
		cfg.SetRXFrequency(440000000),
		cfg.SetTXFrequency(445000000),
		cfg.SetPower(10),
		cfg.SetColorCode(1),
		cfg.SetLatitude([]byte("+30.2672")),
		cfg.SetLongitude([]byte("-097.7431")),
		cfg.SetHeight(10),
		cfg.SetLocation([]byte("Test")),
		cfg.SetDescription([]byte("Session peer")),
		cfg.SetSlots(3),
		cfg.SetSoftwareID([]byte("20260713")),
		cfg.SetPackageID([]byte("MMDVM")),
	)
	if err != nil {
		return nil, fmt.Errorf("building RPTC: %w", err)
	}
	return newSessionPeer(dmrd.DialectMMDVM, id, password, cfg[:], options, retry, timeout)
}

// NewHBRPSessionPeer returns a SessionPeer for the repeater id that speaks
// HBRP, with the 306-octet RPTC and no RPTO.
func NewHBRPSessionPeer(id uint32, password string) (*SessionPeer, error) {
	return NewHBRPSessionPeerCalled(id, password, fmt.Sprintf("H%04dHB", id%10000))
}

// NewHBRPSessionPeerCalled is NewHBRPSessionPeer with the callsign that
// the RPTC carries.
func NewHBRPSessionPeerCalled(id uint32, password, callsign string) (*SessionPeer, error) {
	var cfg dmrd.HBRPConfig
	cfg.Reset()
	cfg.SetID(id)
	err := errors.Join(
		cfg.SetCallsign([]byte(callsign)),
		cfg.SetRXFrequency(438000000),
		cfg.SetTXFrequency(430400000),
		cfg.SetPower(25),
		cfg.SetColorCode(1),
		cfg.SetLatitude([]byte("+52.5200")),
		cfg.SetLongitude([]byte("+013.4050")),
		cfg.SetHeight(30),
		cfg.SetLocation([]byte("Berlin")),
		cfg.SetDescription([]byte("HBRP peer")),
		cfg.SetURL([]byte("https://example.org")),
		cfg.SetSoftwareID([]byte("20261023")),
		cfg.SetPackageID([]byte("HBRP")),
	)
	if err != nil {
		return nil, fmt.Errorf("building RPTC: %w", err)
	}
	return newSessionPeer(dmrd.DialectHBRP, id, password, cfg[:], "", 200*time.Millisecond, 5*time.Second)
}

func newSessionPeer(d dmrd.Dialect, id uint32, password string, cfg []byte, options string, retry, timeout time.Duration) (*SessionPeer, error) {
	peer, err := dmrdsession.NewPeer(&dmrdsession.PeerConfig{
		Dialect:  d,
		ID:       id,
		Password: []byte(password),
		Config:   cfg,
		Options:  []byte(options),
		Retry:    retry,
		Timeout:  timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("making session peer: %w", err)
	}
	return &SessionPeer{
		ID:      id,
		Dialect: d,
		peer:    peer,
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
		packets: make(chan models.Packet, clientBuffer),
	}, nil
}

// Connect starts the login with the master at addr.
func (p *SessionPeer) Connect(addr string) error {
	target, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	p.conn, p.target, p.start = conn, target, time.Now()
	p.wg.Add(2)
	go p.rx()
	go p.ticks()
	return nil
}

// Retarget sends everything from now on to addr from the same socket, as
// when a load balancer moves the repeater to another replica.
func (p *SessionPeer) Retarget(addr string) error {
	target, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	p.mu.Lock()
	p.target = target
	p.mu.Unlock()
	return nil
}

// WaitReady blocks until the login is complete or the timeout expires.
func (p *SessionPeer) WaitReady(timeout time.Duration) error {
	select {
	case <-p.ready:
		return nil
	case <-time.After(timeout):
		return ErrSessionPeerTimeout
	}
}

// State returns the state of the session.
func (p *SessionPeer) State() dmrdsession.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peer.State()
}

// Downs returns how many times the session ended.
func (p *SessionPeer) Downs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.downs
}

// Ups returns how many times the login completed.
func (p *SessionPeer) Ups() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ups
}

// PausePings stops the timers of the Peer, so that it sends no ping and
// nothing else that it is not told to send.
func (p *SessionPeer) PausePings() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.noTicks = true
}

// ResumePings starts the timers of the Peer again after PausePings.
func (p *SessionPeer) ResumePings() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.noTicks = false
}

// ReceivedPackets returns the channel of the DMRD packets from the master.
func (p *SessionPeer) ReceivedPackets() <-chan models.Packet {
	return p.packets
}

// SendRaw sends b to the master as it is.
func (p *SessionPeer) SendRaw(b []byte) error {
	p.mu.Lock()
	target := p.target
	p.mu.Unlock()
	if _, err := p.conn.WriteToUDP(b, target); err != nil {
		return fmt.Errorf("writing raw packet: %w", err)
	}
	return nil
}

// SendDMRD sends a DMRD packet with the repeater ID of the peer.
func (p *SessionPeer) SendDMRD(pkt models.Packet) error {
	pkt.Repeater = uint(p.ID)
	p.mu.Lock()
	target := p.target
	p.mu.Unlock()
	if _, err := p.conn.WriteToUDP(pkt.Encode(), target); err != nil {
		return fmt.Errorf("writing DMRD packet: %w", err)
	}
	return nil
}

// Drain collects the DMRD packets received within timeout.
func (p *SessionPeer) Drain(timeout time.Duration) []models.Packet {
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

// Close ends the session and closes the socket.
func (p *SessionPeer) Close() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		p.run(p.peer.Close(time.Since(p.start), buf[:0]))
		p.mu.Unlock()
		close(p.done)
		_ = p.conn.Close()
	})
	p.wg.Wait()
}

func (p *SessionPeer) run(out []dmrdsession.Output) {
	for _, o := range out {
		switch o.Kind {
		case dmrdsession.OutputSend:
			_, _ = p.conn.WriteToUDP(o.Packet, p.target)
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
			select {
			case p.packets <- models.PacketFromData(&d, d.Size > 53):
			default:
			}
		case dmrdsession.OutputLogin, dmrdsession.OutputConfig, dmrdsession.OutputOptions, dmrdsession.OutputMessage, dmrdsession.OutputPong:
		}
	}
}

func (p *SessionPeer) rx() {
	defer p.wg.Done()
	buf := make([]byte, dmrdsession.MaxPacket)
	for {
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		p.mu.Lock()
		var out [dmrdsession.MaxOutputs]dmrdsession.Output
		p.run(p.peer.Packet(time.Since(p.start), buf[:n], out[:0]))
		p.mu.Unlock()
	}
}

func (p *SessionPeer) ticks() {
	defer p.wg.Done()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.mu.Lock()
			if !p.noTicks {
				var out [dmrdsession.MaxOutputs]dmrdsession.Output
				p.run(p.peer.Tick(time.Since(p.start), out[:0]))
			}
			p.mu.Unlock()
		}
	}
}
