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

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/auth"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
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
	AuthKey  *[20]byte // raw HMAC-SHA1 key, nil when the password is not hex
	Password string    // hex auth key string

	conn       *net.UDPConn
	remoteAddr string
	ready      chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	readyVal       atomic.Bool
	packets        chan IPSCBurst
	disconnected   chan struct{}
	disconnectOnce sync.Once
	rxDone         chan struct{}
	reconnected    chan struct{}
	noReconnect    bool // suppress reconnect on DeregistrationRequest
	rtpSeq         atomic.Uint32
}

var (
	ErrIPSCHandshakeTimeout  = errors.New("IPSC handshake timed out")
	ErrIPSCDeregisterTimeout = errors.New("timed out waiting for IPSC deregistration")
	ErrIPSCNotConnected      = errors.New("IPSC client not connected")
)

// NewIPSCClient creates a new IPSC simulator client.
// password is the hex auth key string as stored in the database.
func NewIPSCClient(peerID uint32, password string) *IPSCClient {
	// Decode the auth key the same way the server does
	authKey := decodeIPSCAuthKey(password)
	return &IPSCClient{
		PeerID:       peerID,
		AuthKey:      authKey,
		Password:     password,
		ready:        make(chan struct{}),
		done:         make(chan struct{}),
		packets:      make(chan IPSCBurst, 100),
		disconnected: make(chan struct{}),
		rxDone:       make(chan struct{}),
		reconnected:  make(chan struct{}),
	}
}

// decodeIPSCAuthKey decodes a hex auth key string into raw bytes, as the
// server does.
func decodeIPSCAuthKey(hexKey string) *[20]byte {
	key, err := ipscauth.ParseKey(hexKey)
	if err != nil {
		return nil
	}
	return &key
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
	c.remoteAddr = addr

	c.wg.Add(1)
	go c.rx()

	// Send registration request
	c.sendMasterRegisterRequest()

	return nil
}

// ConnectWithoutRegistration dials the IPSC server but does NOT send a
// MasterRegisterRequest. The client can still send HMAC-signed packets
// (e.g. voice data) through the UDP socket. This is useful for testing
// that the server rejects traffic from peers that have not completed the
// registration handshake.
func (c *IPSCClient) ConnectWithoutRegistration(addr string) error {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return fmt.Errorf("dial udp: %w", err)
	}
	c.conn = conn
	c.remoteAddr = addr

	c.wg.Add(1)
	go c.rx()

	// Mark as "ready" immediately since we skip handshake on purpose.
	c.readyVal.Store(true)
	close(c.ready)
	// Suppress reconnect: the caller does not want to register.
	c.noReconnect = true

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
	header := c.buildVoicePacket(ipscwire.OpGroupVoice, src, dst, true, slot, streamID, ipscwire.BurstType(enums.DataTypeVoiceLCHeader), false)
	if _, err := c.conn.Write(c.signPacket(header)); err != nil {
		return fmt.Errorf("send voice header: %w", err)
	}

	// Small delay between packets
	time.Sleep(10 * time.Millisecond)

	// Voice burst (Slot1 type)
	burst := c.buildVoicePacket(ipscwire.OpGroupVoice, src, dst, true, slot, streamID, ipscwire.BurstVoice, false)
	if _, err := c.conn.Write(c.signPacket(burst)); err != nil {
		return fmt.Errorf("send voice burst: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Voice terminator
	term := c.buildVoicePacket(ipscwire.OpGroupVoice, src, dst, true, slot, streamID, ipscwire.BurstType(enums.DataTypeTerminatorWithLC), true)
	if _, err := c.conn.Write(c.signPacket(term)); err != nil {
		return fmt.Errorf("send voice term: %w", err)
	}

	return nil
}

// SendPrivateVoice sends an IPSC private voice burst.
func (c *IPSCClient) SendPrivateVoice(src, dst uint, slot bool, streamID uint32) error {
	header := c.buildVoicePacket(ipscwire.OpPrivateVoice, src, dst, false, slot, streamID, ipscwire.BurstType(enums.DataTypeVoiceLCHeader), false)
	if _, err := c.conn.Write(c.signPacket(header)); err != nil {
		return fmt.Errorf("send voice header: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	burst := c.buildVoicePacket(ipscwire.OpPrivateVoice, src, dst, false, slot, streamID, ipscwire.BurstVoice, false)
	if _, err := c.conn.Write(c.signPacket(burst)); err != nil {
		return fmt.Errorf("send voice burst: %w", err)
	}

	time.Sleep(10 * time.Millisecond)

	term := c.buildVoicePacket(ipscwire.OpPrivateVoice, src, dst, false, slot, streamID, ipscwire.BurstType(enums.DataTypeTerminatorWithLC), true)
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

// WaitDisconnected blocks until the client receives a DeregistrationRequest from the server.
func (c *IPSCClient) WaitDisconnected(timeout time.Duration) error {
	select {
	case <-c.disconnected:
		return nil
	case <-time.After(timeout):
		return ErrIPSCDeregisterTimeout
	}
}

// WaitReconnected blocks until the client has completed a re-handshake after
// a deregistration disconnect. Returns ErrIPSCHandshakeTimeout if the
// re-handshake does not complete within the timeout.
func (c *IPSCClient) WaitReconnected(timeout time.Duration) error {
	select {
	case <-c.reconnected:
		return nil
	case <-time.After(timeout):
		return ErrIPSCHandshakeTimeout
	}
}

// reconnect is called asynchronously when a DeregistrationRequest is received.
// It closes the old connection, waits for the rx goroutine to exit, and attempts
// to re-establish the session by dialing and re-handshaking with the same server.
func (c *IPSCClient) reconnect() {
	// Close old connection — rx() will see net.ErrClosed and exit
	if c.conn != nil {
		_ = c.conn.Close()
	}
	// Wait for the old rx goroutine to fully exit
	<-c.rxDone

	// Check if we're being permanently shut down
	select {
	case <-c.done:
		return
	default:
	}

	// Dial new connection to the same server address
	raddr, err := net.ResolveUDPAddr("udp", c.remoteAddr)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}
	c.conn = conn

	// Reset state for new handshake
	c.rxDone = make(chan struct{})
	c.ready = make(chan struct{})
	c.readyVal.Store(false)

	// Start new rx goroutine and send registration request
	c.wg.Add(1)
	go c.rx()
	c.sendMasterRegisterRequest()

	// Wait for the new handshake to complete (or shutdown/timeout)
	select {
	case <-c.ready:
		close(c.reconnected)
	case <-c.done:
	case <-time.After(10 * time.Second):
	}
}

func (c *IPSCClient) rx() {
	defer c.wg.Done()
	defer close(c.rxDone)
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

	// Strip HMAC if present (last 10 bytes)
	payload := data
	if len(data) > ipscauth.TrailerLen {
		payload = data[:len(data)-ipscauth.TrailerLen]
	}

	switch op := ipscwire.Opcode(data[0]); op { //nolint:exhaustive
	case ipscwire.OpRegisterReply:
		if !c.readyVal.Load() {
			c.readyVal.Store(true)
			close(c.ready)
			// Start keepalive
			c.wg.Add(1)
			go c.keepAlive()
		}

	case ipscwire.OpGroupVoice, ipscwire.OpPrivateVoice, ipscwire.OpGroupData, ipscwire.OpPrivateData:
		burst := c.parseIPSCBurst(op, payload)
		select {
		case c.packets <- burst:
		default:
		}

	case ipscwire.OpDeregisterRequest: // server shutting down, signal disconnect and reconnect
		c.disconnectOnce.Do(func() {
			close(c.disconnected)
		})
		if !c.noReconnect {
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				c.reconnect()
			}()
		}

	default: // MasterAliveReply and PeerListReply are ignored
	}
}

func (c *IPSCClient) parseIPSCBurst(op ipscwire.Opcode, data []byte) IPSCBurst {
	burst := IPSCBurst{
		PacketType: byte(op),
		Data:       data,
	}
	if u, err := ipscwire.DecodeUser(data); err == nil {
		burst.PeerID = u.ID
		burst.Src = uint(u.Source)
		burst.Dst = uint(u.Destination)
		burst.GroupCall = op == ipscwire.OpGroupVoice || op == ipscwire.OpGroupData
		burst.Slot = u.Info.Slot() == enums.Slot2
	}
	return burst
}

// peerControl returns a control packet of the client with the mode and
// flags of a MOTOTRBO peer.
func (c *IPSCClient) peerControl(op ipscwire.Opcode) []byte {
	ctl := ipscwire.Control{
		Opcode: op,
		ID:     c.PeerID,
		// Mode: operational + digital + ts1 + ts2
		Mode:    0x6A,
		Flags:   ipscwire.Flags{0x00, 0x00, 0x00, 0x1D},
		Version: [4]byte{0x04, 0x02, 0x04, 0x01},
	}
	pkt, err := ctl.AppendBinary(nil)
	if err != nil {
		return nil
	}
	return c.signPacket(pkt)
}

func (c *IPSCClient) sendMasterRegisterRequest() {
	_, _ = c.conn.Write(c.peerControl(ipscwire.OpRegisterRequest))
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
	_, _ = c.conn.Write(c.peerControl(ipscwire.OpAliveRequest))
}

func (c *IPSCClient) signPacket(data []byte) []byte {
	if c.AuthKey == nil {
		return data
	}
	return ipscauth.NewKey(*c.AuthKey).Sign(data)
}

// buildVoicePacket constructs an IPSC voice user packet as an XPR 8400
// sends it: a voice LC header or terminator with the Full LC of the call,
// or voice burst A with silence. It returns nil when the packet does not
// encode.
func (c *IPSCClient) buildVoicePacket(op ipscwire.Opcode, src, dst uint, groupCall, slot bool, streamID uint32, burstType ipscwire.BurstType, isEnd bool) []byte {
	seq := uint16(c.rtpSeq.Add(1)) //nolint:gosec // the RTP sequence number wraps
	u := ipscwire.User{
		Opcode:      op,
		ID:          c.PeerID,
		Source:      uint32(src),
		Destination: uint32(dst),
		Priority:    0x02,
		CallControl: streamID,
		RTP:         ipscwire.RTP{Flags: 0x80, PayloadType: 0x5D, Seq: seq, Timestamp: 480 * uint32(seq)},
	}
	if slot {
		u.Info |= 0x20
	}
	if isEnd {
		u.Info |= 0x40
		u.RTP.PayloadType = 0x5E
	}

	var raw layer2.RawBurst
	var err error
	if burstType == ipscwire.BurstVoice {
		raw, err = layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{}, enums.VoiceBurstA, pdu.EmbeddedSignalling{}, [4]byte{}, layer2.SyncSourceMS)
		if err == nil {
			err = u.SetRawBurst(raw, layer2.KindVoice, enums.VoiceBurstA, 0)
		}
	} else {
		dt := enums.DataType(burstType)
		var lc pdu.LCBytes
		var cw [12]byte
		lc, err = pdu.NewVoiceLCBytes(groupCall, uint32(dst), uint32(src), layer3.MotorolaServiceOptions())
		if err == nil {
			cw, err = lc.Codeword(dt)
		}
		if err == nil {
			raw, err = layer2.BuildDataBurst(cw, dt, 1, layer2.SyncSourceMS)
		}
		if err == nil {
			err = u.SetRawBurst(raw, layer2.KindData, enums.VoiceBurstUnknown, dt)
		}
	}
	if err != nil {
		return nil
	}
	pkt, err := u.AppendBinary(nil)
	if err != nil {
		return nil
	}
	return pkt
}
