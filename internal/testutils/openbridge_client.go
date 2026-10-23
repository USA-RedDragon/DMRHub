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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
)

const openbridgePacketLength = 73

// OpenBridgeClient is a simulated OpenBridge peer client for integration testing.
// OpenBridge is stateless (no handshake) — packets are authenticated via HMAC-SHA1.
type OpenBridgeClient struct {
	PeerID   uint32
	Password string

	conn     *net.UDPConn
	server   *net.UDPAddr
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	packets  chan models.Packet
	bridges  chan ReceivedBridge
	controls chan dmrd.BridgeControl
}

var ErrOpenBridgeNotConnected = errors.New("OpenBridge client not connected")

// NewOpenBridgeClient creates a new OpenBridge simulator client.
func NewOpenBridgeClient(peerID uint32, password string) *OpenBridgeClient {
	return &OpenBridgeClient{
		PeerID:   peerID,
		Password: password,
		done:     make(chan struct{}),
		packets:  make(chan models.Packet, clientBuffer),
		bridges:  make(chan ReceivedBridge, clientBuffer),
		controls: make(chan dmrd.BridgeControl, clientBuffer),
	}
}

// ReceivedBridge is an OpenBridge data packet that the client got, and the
// address it came from.
type ReceivedBridge struct {
	dmrd.Bridge
	From net.UDPAddr
}

// DrainBridges collects the OpenBridge data packets, of any version, that
// arrive with a good signature within timeout.
func (c *OpenBridgeClient) DrainBridges(timeout time.Duration) []ReceivedBridge {
	var out []ReceivedBridge
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case b := <-c.bridges:
			out = append(out, b)
		case <-timer.C:
			return out
		}
	}
}

// DrainControls collects the control packets, such as BCKA, that arrive
// with a good signature within timeout.
func (c *OpenBridgeClient) DrainControls(timeout time.Duration) []dmrd.BridgeControl {
	var out []dmrd.BridgeControl
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case b := <-c.controls:
			out = append(out, b)
		case <-timer.C:
			return out
		}
	}
}

// Connect opens a socket to send to the OpenBridge server at addr. It
// takes packets from any address, as a peer that several replicas send to
// does. No handshake is needed.
func (c *OpenBridgeClient) Connect(addr string) error {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	c.conn, c.server = conn, raddr

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
	_, err := c.conn.WriteToUDP(data, c.server)
	if err != nil {
		return fmt.Errorf("writing raw bytes: %w", err)
	}
	return nil
}

func (c *OpenBridgeClient) sendPacket(pkt models.Packet) error {
	if c.conn == nil {
		return ErrOpenBridgeNotConnected
	}
	// Only the 53-octet DMRD packet, without BER and RSSI
	bridge := dmrd.Bridge{Version: 1, Data: pkt.Data()}
	bridge.Data.Size = dmrconst.MMDVMPacketLength
	outbound, err := bridge.AppendBinary(nil)
	if err != nil {
		return fmt.Errorf("encoding packet: %w", err)
	}
	if err := dmrdauth.NewBridgeKey([]byte(c.Password)).SignInPlace(outbound); err != nil {
		return fmt.Errorf("signing packet: %w", err)
	}

	_, err = c.conn.WriteToUDP(outbound, c.server)
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
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// timeout — loop back
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])

		if _, ok := dmrdauth.NewBridgeKey([]byte(c.Password)).Verify(data); !ok {
			continue // Invalid HMAC, skip
		}
		bridge, err := dmrd.DecodeBridge(data)
		if err != nil {
			if control, err := dmrd.DecodeBridgeControl(data); err == nil {
				select {
				case c.controls <- control:
				default:
				}
			}
			continue
		}
		select {
		case c.bridges <- ReceivedBridge{Bridge: bridge, From: *from}:
		default:
		}
		if n != openbridgePacketLength {
			continue
		}
		pkt := models.PacketFromData(&bridge.Data, false)
		select {
		case c.packets <- pkt:
		default:
			// drop if channel full
		}
	}
}

// SendEnhancedPacket sends pkt as a signed DMRE packet of FreeDMR's
// enhanced OpenBridge, version 4 or 5.
func (c *OpenBridgeClient) SendEnhancedPacket(pkt models.Packet, version uint8) error {
	return c.SendBridge(dmrd.Bridge{Version: version, Data: pkt.Data(), SourceServer: c.PeerID, SourceRepeater: uint32(pkt.Repeater), Hops: 1}) //nolint:gosec
}

// SendBridge sends bridge, a DMRE packet of version 4 or 5 with its source
// server, repeater and hops, signed, with the send time of now.
func (c *OpenBridgeClient) SendBridge(bridge dmrd.Bridge) error {
	bridge.Time = uint64(time.Now().UnixNano()) //nolint:gosec
	bridge.Data.Size = dmrconst.MMDVMMaxPacketLength
	out, err := bridge.AppendBinary(nil)
	if err != nil {
		return fmt.Errorf("encoding packet: %w", err)
	}
	return c.sendSigned(out)
}

// SendControl sends a signed OpenBridge control packet.
func (c *OpenBridgeClient) SendControl(control dmrd.BridgeControl) error {
	out, err := control.AppendBinary(nil)
	if err != nil {
		return fmt.Errorf("encoding control packet: %w", err)
	}
	return c.sendSigned(out)
}

// SendControlTo sends a signed control packet to the OpenBridge server at
// addr, such as another replica, from the same socket.
func (c *OpenBridgeClient) SendControlTo(addr string, control dmrd.BridgeControl) error {
	to, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	out, err := control.AppendBinary(nil)
	if err != nil {
		return fmt.Errorf("encoding control packet: %w", err)
	}
	return c.sendSignedTo(out, to)
}

func (c *OpenBridgeClient) sendSigned(p []byte) error {
	return c.sendSignedTo(p, c.server)
}

func (c *OpenBridgeClient) sendSignedTo(p []byte, to *net.UDPAddr) error {
	if c.conn == nil {
		return ErrOpenBridgeNotConnected
	}
	if err := dmrdauth.NewBridgeKey([]byte(c.Password)).SignInPlace(p); err != nil {
		return fmt.Errorf("signing packet: %w", err)
	}
	if _, err := c.conn.WriteToUDP(p, to); err != nil {
		return fmt.Errorf("writing packet: %w", err)
	}
	return nil
}
