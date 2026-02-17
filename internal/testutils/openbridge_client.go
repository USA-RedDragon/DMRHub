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
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
)

const openbridgePacketLength = 73

// OpenBridgeClient is a simulated OpenBridge peer client for integration testing.
// OpenBridge is stateless (no handshake) — packets are authenticated via HMAC-SHA1.
type OpenBridgeClient struct {
	PeerID   uint32
	Password string

	conn     *net.UDPConn
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	packets  chan models.Packet
}

var ErrOpenBridgeNotConnected = errors.New("OpenBridge client not connected")

// NewOpenBridgeClient creates a new OpenBridge simulator client.
func NewOpenBridgeClient(peerID uint32, password string) *OpenBridgeClient {
	return &OpenBridgeClient{
		PeerID:   peerID,
		Password: password,
		done:     make(chan struct{}),
		packets:  make(chan models.Packet, 100),
	}
}

// Connect dials the OpenBridge server. No handshake is needed.
func (c *OpenBridgeClient) Connect(addr string) error {
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

	return nil
}

// LocalAddr returns the local UDP address of the client.
// Useful for configuring the peer's IP/Port in the DB for outbound delivery.
func (c *OpenBridgeClient) LocalAddr() *net.UDPAddr {
	if c.conn == nil {
		return nil
	}
	addr, ok := c.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return addr
}

// ReceivedPackets returns a channel of packets received from the server.
func (c *OpenBridgeClient) ReceivedPackets() <-chan models.Packet {
	return c.packets
}

// Drain collects all packets received within the given timeout window.
func (c *OpenBridgeClient) Drain(timeout time.Duration) []models.Packet {
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

// SendGroupVoice sends an OpenBridge group voice packet (voice head + voice + terminator).
func (c *OpenBridgeClient) SendGroupVoice(src, dst uint, streamID uint32) error {
	if c.conn == nil {
		return ErrOpenBridgeNotConnected
	}

	// Voice head
	head := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         0,
		Src:         src,
		Dst:         dst,
		Repeater:    uint(c.PeerID),
		GroupCall:   true,
		Slot:        false, // OpenBridge is always TS1
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceHead),
		StreamID:    uint(streamID),
		BER:         -1,
		RSSI:        -1,
	}
	if err := c.sendPacket(head); err != nil {
		return fmt.Errorf("send voice head: %w", err)
	}

	// Voice A
	voice := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         1,
		Src:         src,
		Dst:         dst,
		Repeater:    uint(c.PeerID),
		GroupCall:   true,
		Slot:        false,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    uint(streamID),
		BER:         -1,
		RSSI:        -1,
	}
	if err := c.sendPacket(voice); err != nil {
		return fmt.Errorf("send voice: %w", err)
	}

	// Terminator
	term := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         2,
		Src:         src,
		Dst:         dst,
		Repeater:    uint(c.PeerID),
		GroupCall:   true,
		Slot:        false,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(dmrconst.DTypeVoiceTerm),
		StreamID:    uint(streamID),
		BER:         -1,
		RSSI:        -1,
	}
	if err := c.sendPacket(term); err != nil {
		return fmt.Errorf("send term: %w", err)
	}

	return nil
}

// SendRawPacket sends a single DMRD packet with HMAC appended.
func (c *OpenBridgeClient) SendRawPacket(pkt models.Packet) error {
	return c.sendPacket(pkt)
}

// SendRawBytes sends raw bytes over the UDP connection (no HMAC added).
// Useful for testing invalid HMAC scenarios.
func (c *OpenBridgeClient) SendRawBytes(data []byte) error {
	if c.conn == nil {
		return ErrOpenBridgeNotConnected
	}
	_, err := c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("writing raw bytes: %w", err)
	}
	return nil
}

func (c *OpenBridgeClient) sendPacket(pkt models.Packet) error {
	if c.conn == nil {
		return ErrOpenBridgeNotConnected
	}
	encoded := pkt.Encode()
	// Only use the first 53 bytes (DMRD packet, no BER/RSSI)
	packetBytes := encoded[:dmrconst.MMDVMPacketLength]

	// Compute HMAC-SHA1
	h := hmac.New(sha1.New, []byte(c.Password))
	_, err := h.Write(packetBytes)
	if err != nil {
		return fmt.Errorf("computing HMAC: %w", err)
	}
	outbound := slices.Concat(packetBytes, h.Sum(nil))

	_, err = c.conn.Write(outbound)
	if err != nil {
		return fmt.Errorf("writing packet: %w", err)
	}
	return nil
}

// Close shuts down the client.
func (c *OpenBridgeClient) Close() {
	c.stopOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	c.wg.Wait()
}

func (c *OpenBridgeClient) rx() {
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
		if n != openbridgePacketLength {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])

		packetBytes := data[:dmrconst.MMDVMPacketLength]
		hmacBytes := data[dmrconst.MMDVMPacketLength:]

		// Validate HMAC
		h := hmac.New(sha1.New, []byte(c.Password))
		_, _ = h.Write(packetBytes)
		if !hmac.Equal(h.Sum(nil), hmacBytes) {
			continue // Invalid HMAC, skip
		}

		pkt, ok := models.UnpackPacket(packetBytes)
		if !ok {
			continue
		}
		select {
		case c.packets <- pkt:
		default:
			// drop if channel full
		}
	}
}
