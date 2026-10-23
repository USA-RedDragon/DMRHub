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
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
)

// MMDVMClient is a simulated MMDVM repeater client for integration testing.
// It performs the full Homebrew protocol handshake (RPTL → RPTACK → RPTK → RPTACK → RPTC → RPTACK)
// and can send/receive DMRD voice packets over UDP.
type MMDVMClient struct {
	RepeaterID uint32
	Callsign   string
	Password   string

	conn     *net.UDPConn
	ready    chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	state    atomic.Uint32
	packets  chan models.Packet
	readyVal atomic.Bool
	pongs    atomic.Uint64
	noPings  atomic.Bool
}

const (
	mmdvmStateIdle       = 0
	mmdvmStateSentLogin  = 1
	mmdvmStateSentAuth   = 2
	mmdvmStateSentConfig = 3
	mmdvmStateReady      = 4
)

var ErrMMDVMHandshakeTimeout = errors.New("MMDVM handshake timed out")

// NewMMDVMClient creates a new MMDVM simulator client.
func NewMMDVMClient(repeaterID uint32, callsign, password string) *MMDVMClient {
	return &MMDVMClient{
		RepeaterID: repeaterID,
		Callsign:   callsign,
		Password:   password,
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
		packets:    make(chan models.Packet, clientBuffer),
	}
}

// Connect dials the MMDVM server and starts the handshake.
func (c *MMDVMClient) Connect(addr string) error {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return fmt.Errorf("dial udp: %w", err)
	}
	c.conn = conn

	c.wg.Add(1)
	go c.rx()

	// Send login
	c.state.Store(mmdvmStateSentLogin)
	c.sendLogin()

	return nil
}

// WaitReady blocks until the handshake is complete or the timeout expires.
func (c *MMDVMClient) WaitReady(timeout time.Duration) error {
	select {
	case <-c.ready:
		return nil
	case <-time.After(timeout):
		return ErrMMDVMHandshakeTimeout
	}
}

// ReceivedPackets returns a channel of DMRD packets received from the server.
func (c *MMDVMClient) ReceivedPackets() <-chan models.Packet {
	return c.packets
}

// Drain collects all packets received within the given timeout window.
// Useful for negative assertions (proving nothing extra was delivered).
func (c *MMDVMClient) Drain(timeout time.Duration) []models.Packet {
	var result []models.Packet
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case pkt := <-c.packets:
			result = append(result, pkt)
		case <-timer.C:
			return result
		}
	}
}

// SendDMRD sends a raw DMRD voice packet to the server.
func (c *MMDVMClient) SendDMRD(pkt models.Packet) error {
	pkt.Repeater = uint(c.RepeaterID)
	data := pkt.Encode()
	_, err := c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("writing DMRD packet: %w", err)
	}
	return nil
}

// SendRaw sends p to the server as it is, for packets that SendDMRD cannot
// build, such as DMRA, DMRG and malformed packets.
func (c *MMDVMClient) SendRaw(p []byte) error {
	if _, err := c.conn.Write(p); err != nil {
		return fmt.Errorf("writing raw packet: %w", err)
	}
	return nil
}

// SendOptions sends an RPTO with the options text for the repeater ID of
// the client.
func (c *MMDVMClient) SendOptions(text string) error {
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: c.RepeaterID, Text: []byte(text)}).AppendBinary(nil, dmrd.DialectMMDVM)
	if err != nil {
		return fmt.Errorf("building RPTO: %w", err)
	}
	return c.SendRaw(p)
}

// Ping sends an RPTPING and reports whether an MSTPONG came back within
// timeout, that is whether the session is still up.
func (c *MMDVMClient) Ping(timeout time.Duration) bool {
	before := c.pongs.Load()
	c.sendPing()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.pongs.Load() > before {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Close shuts down the client.
func (c *MMDVMClient) Close() {
	c.stopOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	c.wg.Wait()
}

func (c *MMDVMClient) rx() {
	defer c.wg.Done()
	buf := make([]byte, 512)
	for {
		select {
		case <-c.done:
			return
		default:
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := c.conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// timeout — loop back
			continue
		}
		if n < 4 {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])

		c.handlePacket(data)
	}
}

func (c *MMDVMClient) handlePacket(data []byte) {
	command := dmrd.Classify(data, dmrd.DialectMMDVM)
	var msg dmrd.Control
	if command == dmrd.CommandRPTACK {
		var err error
		if msg, err = dmrd.DecodeControl(data, dmrd.DialectMMDVM); err != nil {
			return
		}
	}

	switch c.state.Load() {
	case mmdvmStateSentLogin:
		// Expecting RPTACK with the salt
		if salt, ok := msg.LoginSalt(); ok {
			c.sendRPTK(salt)
			c.state.Store(mmdvmStateSentAuth)
		}

	case mmdvmStateSentAuth:
		// Expecting RPTACK to confirm auth
		if command == dmrd.CommandRPTACK {
			c.sendRPTC()
			c.state.Store(mmdvmStateSentConfig)
		}

	case mmdvmStateSentConfig:
		// Expecting RPTACK to confirm config
		if command == dmrd.CommandRPTACK {
			c.state.Store(mmdvmStateReady)
			c.readyVal.Store(true)
			close(c.ready)
			// Start keepalive
			c.wg.Add(1)
			go c.ping()
		}

	case mmdvmStateReady:
		if command == dmrd.CommandMSTPONG {
			c.pongs.Add(1)
		}
		if command == dmrd.CommandDMRD {
			pkt, ok := models.UnpackPacket(data)
			if ok {
				select {
				case c.packets <- pkt:
				default:
					// drop if channel full
				}
			}
		}
		// MSTPONG, MSTCL, RPTSBKN and the rest are ignored.
	}
}

func (c *MMDVMClient) sendControl(command dmrd.Command, id uint32, token [32]byte) {
	data, err := (&dmrd.Control{Command: command, ID: id, Token: token}).AppendBinary(nil, dmrd.DialectMMDVM)
	if err != nil {
		return
	}
	_, _ = c.conn.Write(data)
}

func (c *MMDVMClient) sendLogin() {
	c.sendControl(dmrd.CommandRPTL, c.RepeaterID, [32]byte{})
}

func (c *MMDVMClient) sendRPTK(salt uint32) {
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, salt, []byte(c.Password))
	if err != nil {
		return
	}
	c.sendControl(dmrd.CommandRPTK, c.RepeaterID, token)
}

func (c *MMDVMClient) sendRPTC() {
	callsign := c.Callsign
	if callsign == "" {
		callsign = fmt.Sprintf("T%04dST", c.RepeaterID%10000)
	}

	var cfg dmrd.MMDVMConfig
	cfg.Reset()
	cfg.SetID(c.RepeaterID)
	err := errors.Join(
		cfg.SetCallsign([]byte(callsign)),
		cfg.SetRXFrequency(440000000),
		cfg.SetTXFrequency(440000000),
		cfg.SetPower(10),
		cfg.SetColorCode(1),
		cfg.SetLatitude([]byte(fmt.Sprintf("%+08.4f", 0.0))),
		cfg.SetLongitude([]byte(fmt.Sprintf("%+09.4f", 0.0))),
		cfg.SetHeight(10),
		cfg.SetLocation([]byte("Test")),
		cfg.SetDescription([]byte("Integration")),
		cfg.SetSlots(3),
		cfg.SetSoftwareID([]byte("20210921")),
		cfg.SetPackageID([]byte("TestPackage")),
	)
	if err != nil {
		return
	}
	_, _ = c.conn.Write(cfg[:])
}

// PausePings stops the keepalive pings, so that the client sends nothing
// that it is not told to send.
func (c *MMDVMClient) PausePings() {
	c.noPings.Store(true)
}

func (c *MMDVMClient) ping() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	c.sendPing()
	for {
		select {
		case <-ticker.C:
			if !c.noPings.Load() {
				c.sendPing()
			}
		case <-c.done:
			return
		}
	}
}

func (c *MMDVMClient) sendPing() {
	c.sendControl(dmrd.CommandRPTPING, c.RepeaterID, [32]byte{})
}
