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
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// IPSCBurst represents a raw IPSC user packet received by the simulator client.
type IPSCBurst struct {
	PacketType byte
	PeerID     uint32
	Src        uint
	Dst        uint
	GroupCall  bool
	Slot       bool
	Data       []byte
}

// IPSCClient is a simulated IPSC peer client for integration testing.
// It performs the full IPSC registration handshake (MasterRegisterRequest → MasterRegisterReply)
// and can send/receive IPSC voice burst packets over UDP.
type IPSCClient struct {
	PeerID   uint32
	AuthKey  []byte // raw HMAC-SHA1 key (20 bytes, from decodeAuthKey)
	Password string // hex auth key string

	conn     *net.UDPConn
	ready    chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	readyVal atomic.Bool
	packets  chan IPSCBurst
}

var ErrIPSCHandshakeTimeout = errors.New("IPSC handshake timed out")

// NewIPSCClient creates a new IPSC simulator client.
// password is the hex auth key string as stored in the database.
func NewIPSCClient(peerID uint32, password string) *IPSCClient {
	// Decode the auth key the same way the server does
	authKey := decodeIPSCAuthKey(password)
	return &IPSCClient{
		PeerID:   peerID,
		AuthKey:  authKey,
		Password: password,
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		packets:  make(chan IPSCBurst, 100),
	}
}

// decodeIPSCAuthKey decodes a hex auth key string into raw bytes.
// Mirrors the server's decodeAuthKey function.
func decodeIPSCAuthKey(hexKey string) []byte {
	for len(hexKey) < 40 {
		hexKey = "0" + hexKey
	}
	key := make([]byte, 20)
	for i := 0; i < 20; i++ {
		var val byte
		for j := 0; j < 2; j++ {
			c := hexKey[i*2+j]
			switch {
			case c >= '0' && c <= '9':
				val = val*16 + (c - '0')
			case c >= 'a' && c <= 'f':
				val = val*16 + (c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				val = val*16 + (c - 'A' + 10)
			}
		}
		key[i] = val
	}
	return key
}

// Connect dials the IPSC server and starts the registration handshake.
func (c *IPSCClient) Connect(addr string) error {
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

	// Send registration request
	c.sendMasterRegisterRequest()

	return nil
}

// WaitReady blocks until the registration handshake is complete.
func (c *IPSCClient) WaitReady(timeout time.Duration) error {
	select {
	case <-c.ready:
		return nil
	case <-time.After(timeout):
		return ErrIPSCHandshakeTimeout
	}
}

// ReceivedPackets returns a channel of IPSC bursts received from the server.
func (c *IPSCClient) ReceivedPackets() <-chan IPSCBurst {
	return c.packets
}

// Drain collects all packets received within the given timeout window.
func (c *IPSCClient) Drain(timeout time.Duration) []IPSCBurst {
	var result []IPSCBurst
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

// SendGroupVoice sends an IPSC group voice burst with a voice LC header, a voice burst, and a terminator.
func (c *IPSCClient) SendGroupVoice(src, dst uint, slot bool, streamID uint32) error {
	// Voice header
	header := c.buildVoicePacket(0x80, src, dst, true, slot, streamID, 0x01, false)
	if _, err := c.conn.Write(c.signPacket(header)); err != nil {
		return fmt.Errorf("send voice header: %w", err)
	}

	// Small delay between packets
	time.Sleep(10 * time.Millisecond)

	// Voice burst (Slot1 type)
	burst := c.buildVoicePacket(0x80, src, dst, true, slot, streamID, 0x0A, false)
	if _, err := c.conn.Write(c.signPacket(burst)); err != nil {
		return fmt.Errorf("send voice burst: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Voice terminator
	term := c.buildVoicePacket(0x80, src, dst, true, slot, streamID, 0x02, true)
	if _, err := c.conn.Write(c.signPacket(term)); err != nil {
		return fmt.Errorf("send voice term: %w", err)
	}

	return nil
}

// SendPrivateVoice sends an IPSC private voice burst.
func (c *IPSCClient) SendPrivateVoice(src, dst uint, slot bool, streamID uint32) error {
	header := c.buildVoicePacket(0x81, src, dst, false, slot, streamID, 0x01, false)
	if _, err := c.conn.Write(c.signPacket(header)); err != nil {
		return fmt.Errorf("send voice header: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	burst := c.buildVoicePacket(0x81, src, dst, false, slot, streamID, 0x0A, false)
	if _, err := c.conn.Write(c.signPacket(burst)); err != nil {
		return fmt.Errorf("send voice burst: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	term := c.buildVoicePacket(0x81, src, dst, false, slot, streamID, 0x02, true)
	if _, err := c.conn.Write(c.signPacket(term)); err != nil {
		return fmt.Errorf("send voice term: %w", err)
	}

	return nil
}

// Close shuts down the client.
func (c *IPSCClient) Close() {
	c.stopOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	c.wg.Wait()
}

func (c *IPSCClient) rx() {
	defer c.wg.Done()
	buf := make([]byte, 1500)
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
			continue
		}
		if n < 1 {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])

		c.handlePacket(data)
	}
}

func (c *IPSCClient) handlePacket(data []byte) {
	if len(data) < 1 {
		return
	}

	packetType := data[0]

	// Strip HMAC if present (last 10 bytes)
	payload := data
	if len(data) > 10 {
		payload = data[:len(data)-10]
	}

	switch packetType {
	case 0x91: // MasterRegisterReply
		if !c.readyVal.Load() {
			c.readyVal.Store(true)
			close(c.ready)
			// Start keepalive
			c.wg.Add(1)
			go c.keepAlive()
		}

	case 0x97: // MasterAliveReply — ignore

	case 0x93: // PeerListReply — ignore

	case 0x80, 0x81, 0x83, 0x84: // Voice/data packets
		burst := c.parseIPSCBurst(packetType, payload)
		select {
		case c.packets <- burst:
		default:
		}
	}
}

func (c *IPSCClient) parseIPSCBurst(packetType byte, data []byte) IPSCBurst {
	burst := IPSCBurst{
		PacketType: packetType,
		Data:       data,
	}
	if len(data) >= 12 {
		burst.PeerID = binary.BigEndian.Uint32(data[1:5])
		burst.Src = uint(data[6])<<16 | uint(data[7])<<8 | uint(data[8])
		burst.Dst = uint(data[9])<<16 | uint(data[10])<<8 | uint(data[11])
		burst.GroupCall = packetType == 0x80 || packetType == 0x83
	}
	if len(data) >= 18 {
		burst.Slot = (data[17] & 0x20) != 0
	}
	return burst
}

func (c *IPSCClient) sendMasterRegisterRequest() {
	// Packet: [0x90][PeerID 4B][Mode 1B][Flags 4B]
	pkt := make([]byte, 10)
	pkt[0] = 0x90
	binary.BigEndian.PutUint32(pkt[1:5], c.PeerID)
	// Mode: operational + digital + ts1 + ts2
	pkt[5] = 0x6A
	// Flags
	pkt[6] = 0x00
	pkt[7] = 0x00
	pkt[8] = 0x00
	pkt[9] = 0x1D

	signed := c.signPacket(pkt)
	_, _ = c.conn.Write(signed)
}

func (c *IPSCClient) keepAlive() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	c.sendMasterAliveRequest()
	for {
		select {
		case <-ticker.C:
			c.sendMasterAliveRequest()
		case <-c.done:
			return
		}
	}
}

func (c *IPSCClient) sendMasterAliveRequest() {
	pkt := make([]byte, 10)
	pkt[0] = 0x96
	binary.BigEndian.PutUint32(pkt[1:5], c.PeerID)
	pkt[5] = 0x6A
	pkt[6] = 0x00
	pkt[7] = 0x00
	pkt[8] = 0x00
	pkt[9] = 0x1D

	signed := c.signPacket(pkt)
	_, _ = c.conn.Write(signed)
}

func (c *IPSCClient) signPacket(data []byte) []byte {
	if c.AuthKey == nil {
		return data
	}
	h := hmac.New(sha1.New, c.AuthKey)
	h.Write(data)
	hashSum := h.Sum(nil)[:10]
	return append(data, hashSum...)
}

// buildVoicePacket constructs an IPSC voice/data user packet.
// burstType: 0x01=VoiceHead, 0x0A=Slot1Voice, 0x02=VoiceTerm
func (c *IPSCClient) buildVoicePacket(packetType byte, src, dst uint, groupCall, slot bool, streamID uint32, burstType byte, isEnd bool) []byte {
	// Build a 54-byte packet for headers/terminators, 57 for voice bursts
	size := 54
	if burstType == 0x0A || burstType == 0x8A {
		size = 57
	}

	pkt := make([]byte, size)

	// Byte 0: Packet type
	pkt[0] = packetType

	// Bytes 1-4: Peer ID
	binary.BigEndian.PutUint32(pkt[1:5], c.PeerID)

	// Byte 5: Sequence
	pkt[5] = 0x00

	// Bytes 6-8: Source
	pkt[6] = byte(src >> 16)
	pkt[7] = byte(src >> 8)
	pkt[8] = byte(src)

	// Bytes 9-11: Destination
	pkt[9] = byte(dst >> 16)
	pkt[10] = byte(dst >> 8)
	pkt[11] = byte(dst)

	// Byte 12: Priority/flags
	pkt[12] = 0x00

	// Bytes 13-16: Call control (use streamID)
	binary.BigEndian.PutUint32(pkt[13:17], streamID)

	// Byte 17: Call info (slot + end flag)
	callInfo := byte(0x00)
	if slot {
		callInfo |= 0x20
	}
	if isEnd {
		callInfo |= 0x40
	}
	pkt[17] = callInfo

	// Bytes 18-29: RTP header (12 bytes) — fill with minimal valid values
	pkt[18] = 0x80 // RTP version 2
	pkt[19] = 0x5D // Payload type
	// RTP seq (bytes 20-21)
	pkt[20] = 0x00
	pkt[21] = 0x01
	// RTP timestamp (bytes 22-25)
	binary.BigEndian.PutUint32(pkt[22:26], 480)
	// RTP SSRC (bytes 26-29)
	binary.BigEndian.PutUint32(pkt[26:30], streamID)

	// Byte 30: Burst type
	pkt[30] = burstType

	// Byte 31: Length
	if burstType == 0x01 || burstType == 0x02 {
		pkt[31] = 0x16 // 22 bytes follow
	} else {
		pkt[31] = 0x19 // 25 bytes follow
	}

	// Byte 32: Unknown
	pkt[32] = 0x06

	// Bytes 33-51: AMBE data (19 bytes of silence)
	// Leave as zeros for test purposes

	// For voice header/term: bytes 38-49 are LC data (12 bytes)
	if burstType == 0x01 || burstType == 0x02 {
		// FLCO
		if groupCall {
			pkt[38] = 0x00 // Group voice
		} else {
			pkt[38] = 0x03 // Unit to unit
		}
		pkt[39] = 0x00
		pkt[40] = 0x20 // Service options
		// Destination (bytes 41-43)
		pkt[41] = byte(dst >> 16)
		pkt[42] = byte(dst >> 8)
		pkt[43] = byte(dst)
		// Source (bytes 44-46)
		pkt[44] = byte(src >> 16)
		pkt[45] = byte(src >> 8)
		pkt[46] = byte(src)
	}

	return pkt
}
