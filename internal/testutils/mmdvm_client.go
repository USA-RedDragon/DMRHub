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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
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
		packets:    make(chan models.Packet, 100),
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
	sig := string(data[:4])

	switch c.state.Load() {
	case mmdvmStateSentLogin:
		// Expecting RPTACK with 4-byte salt
		if len(data) >= 6 && string(data[:6]) == string(dmrconst.CommandRPTACK) {
			if len(data) < 10 {
				return
			}
			salt := data[len(data)-4:]
			c.sendRPTK(salt)
			c.state.Store(mmdvmStateSentAuth)
		}

	case mmdvmStateSentAuth:
		// Expecting RPTACK to confirm auth
		if len(data) >= 6 && string(data[:6]) == string(dmrconst.CommandRPTACK) {
			c.sendRPTC()
			c.state.Store(mmdvmStateSentConfig)
		}

	case mmdvmStateSentConfig:
		// Expecting RPTACK to confirm config
		if len(data) >= 6 && string(data[:6]) == string(dmrconst.CommandRPTACK) {
			c.state.Store(mmdvmStateReady)
			c.readyVal.Store(true)
			close(c.ready)
			// Start keepalive
			c.wg.Add(1)
			go c.ping()
		}

	case mmdvmStateReady:
		switch dmrconst.Command(sig) {
		case dmrconst.CommandDMRD:
			pkt, ok := models.UnpackPacket(data)
			if ok {
				select {
				case c.packets <- pkt:
				default:
					// drop if channel full
				}
			}
		case dmrconst.CommandMSTPONG:
			// MSTPONG — ignore
		case dmrconst.CommandMSTCL:
			// MSTCL — server closing
		case dmrconst.CommandRPTSBKN:
			// RPTSBKN — beacon, ignore
		case dmrconst.CommandDMRA, dmrconst.CommandMSTNAK, dmrconst.CommandRPTL, dmrconst.CommandRPTPING, dmrconst.CommandRPTACK, dmrconst.CommandRPTK, dmrconst.CommandRPTC, dmrconst.CommandRPTO, dmrconst.CommandRPTCL:
			// Unexpected commands in ready state — ignore
		}
	}
}

func (c *MMDVMClient) sendLogin() {
	data := make([]byte, 8)
	copy(data[0:4], []byte(dmrconst.CommandRPTL))
	binary.BigEndian.PutUint32(data[4:8], c.RepeaterID)
	_, _ = c.conn.Write(data)
}

func (c *MMDVMClient) sendRPTK(salt []byte) {
	s256 := sha256.New()
	s256.Write(salt)
	s256.Write([]byte(c.Password))
	token := s256.Sum(nil)

	buf := make([]byte, 40)
	copy(buf[0:4], dmrconst.CommandRPTK)
	binary.BigEndian.PutUint32(buf[4:8], c.RepeaterID)
	copy(buf[8:], token)
	_, _ = c.conn.Write(buf)
}

func (c *MMDVMClient) sendRPTC() {
	str := make([]byte, 0, 302)
	str = append(str, []byte(dmrconst.CommandRPTC)...)
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, c.RepeaterID)
	str = append(str, idBytes...)

	callsign := c.Callsign
	if callsign == "" {
		callsign = fmt.Sprintf("T%04dST", c.RepeaterID%10000)
	}

	str = append(str, []byte(fmt.Sprintf("%-8s", callsign))...)       // 8:16
	str = append(str, []byte(fmt.Sprintf("%09d", 440000000))...)      // 16:25 rxfreq
	str = append(str, []byte(fmt.Sprintf("%09d", 440000000))...)      // 25:34 txfreq
	str = append(str, []byte(fmt.Sprintf("%02d", 10))...)             // 34:36 txpower
	str = append(str, []byte(fmt.Sprintf("%02d", 1))...)              // 36:38 color code
	str = append(str, []byte(fmt.Sprintf("%+08.4f", 0.0))...)         // 38:46 lat
	str = append(str, []byte(fmt.Sprintf("%+09.4f", 0.0))...)         // 46:55 lon
	str = append(str, []byte(fmt.Sprintf("%03d", 10))...)             // 55:58 height
	str = append(str, []byte(fmt.Sprintf("%-20s", "Test"))...)        // 58:78 location
	str = append(str, []byte(fmt.Sprintf("%-19s", "Integration"))...) // 78:97 description
	str = append(str, []byte("3")...)                                 // 97:98 slots
	str = append(str, []byte(fmt.Sprintf("%-124s", ""))...)           // 98:222 url
	str = append(str, []byte(fmt.Sprintf("%-40s", "20210921"))...)    // 222:262 softwareID
	str = append(str, []byte(fmt.Sprintf("%-40s", "TestPackage"))...) // 262:302 packageID

	_, _ = c.conn.Write(str)
}

func (c *MMDVMClient) ping() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	c.sendPing()
	for {
		select {
		case <-ticker.C:
			c.sendPing()
		case <-c.done:
			return
		}
	}
}

func (c *MMDVMClient) sendPing() {
	data := make([]byte, 11)
	copy(data[0:7], []byte(dmrconst.CommandRPTPING))
	binary.BigEndian.PutUint32(data[7:11], c.RepeaterID)
	_, _ = c.conn.Write(data)
}
