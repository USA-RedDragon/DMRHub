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

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscauth"
	ipscsession "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscsession"
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

// IPSCClient is a simulated IPSC peer for integration testing. It
// registers with the master through the dmrgo session Peer and sends and
// receives IPSC voice packets over UDP.
type IPSCClient struct {
	PeerID   uint32
	Password string

	key     [20]byte
	useAuth bool

	mu      sync.Mutex
	peer    *ipscsession.Peer
	conn    *net.UDPConn
	master  *net.UDPAddr
	start   time.Time
	started bool
	rtpSeq  uint16

	ready    chan struct{}
	upOnce   sync.Once
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	packets  chan IPSCBurst
}

var (
	ErrIPSCHandshakeTimeout = errors.New("IPSC handshake timed out")
	ErrIPSCNotConnected     = errors.New("IPSC client not connected")
)

// NewIPSCClient creates a new IPSC simulator client. password is the hex
// auth key as stored in the database; a password that is not hex turns
// authentication off.
func NewIPSCClient(peerID uint32, password string) *IPSCClient {
	key, err := ipscauth.ParseKey(password)
	return &IPSCClient{
		PeerID:   peerID,
		Password: password,
		key:      key,
		useAuth:  err == nil,
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		packets:  make(chan IPSCBurst, 100),
	}
}

func (c *IPSCClient) dial(addr string) error {
	master, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	if master.IP == nil || master.IP.IsUnspecified() {
		master.IP = net.IPv4(127, 0, 0, 1)
	}
	to, ok := ipscsession.AddrOf(master.IP, master.Port)
	if !ok {
		return fmt.Errorf("master address %v is not IPv4: %w", master, ErrIPSCNotConnected)
	}
	peer, err := ipscsession.NewPeer(&ipscsession.PeerConfig{
		ID:     c.PeerID,
		Master: to,
		Auth:   c.useAuth,
		Key:    c.key,
		Alive:  time.Second,
	})
	if err != nil {
		return fmt.Errorf("making IPSC peer: %w", err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	c.peer, c.conn, c.master, c.start = peer, conn, master, time.Now()
	c.wg.Add(1)
	go c.rx()
	return nil
}

// Connect starts the registration with the master at addr.
func (c *IPSCClient) Connect(addr string) error {
	if err := c.dial(addr); err != nil {
		return err
	}
	c.startSession()
	return nil
}

// ConnectWithoutRegistration opens a socket to the master at addr without
// a registration, so the client can send signed voice packets as a peer
// that registered with another master does. Drain then registers the
// client, so that it can receive.
func (c *IPSCClient) ConnectWithoutRegistration(addr string) error {
	if err := c.dial(addr); err != nil {
		return err
	}
	c.upOnce.Do(func() { close(c.ready) })
	return nil
}

func (c *IPSCClient) startSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return
	}
	c.started = true
	c.wg.Add(1)
	go c.ticks()
}

// WaitReady blocks until the registration is complete.
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
	c.startSession()
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

// SendGroupVoice sends an IPSC group voice call: a voice LC header, a voice
// burst and a terminator.
func (c *IPSCClient) SendGroupVoice(src, dst uint, slot bool, streamID uint32) error {
	return c.sendVoice(ipscwire.OpGroupVoice, src, dst, true, slot, streamID)
}

// SendPrivateVoice sends an IPSC private voice call.
func (c *IPSCClient) SendPrivateVoice(src, dst uint, slot bool, streamID uint32) error {
	return c.sendVoice(ipscwire.OpPrivateVoice, src, dst, false, slot, streamID)
}

func (c *IPSCClient) sendVoice(op ipscwire.Opcode, src, dst uint, groupCall, slot bool, streamID uint32) error {
	if c.conn == nil {
		return ErrIPSCNotConnected
	}
	for i, burst := range []ipscwire.BurstType{
		ipscwire.BurstType(enums.DataTypeVoiceLCHeader),
		ipscwire.BurstVoice,
		ipscwire.BurstType(enums.DataTypeTerminatorWithLC),
	} {
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		c.mu.Lock()
		p := c.buildVoicePacket(op, src, dst, groupCall, slot, streamID, burst, i == 2)
		if p != nil {
			var err error
			if p, err = c.peer.Sign(p); err != nil {
				c.mu.Unlock()
				return fmt.Errorf("signing voice packet %d: %w", i, err)
			}
		}
		c.mu.Unlock()
		if p == nil {
			return fmt.Errorf("building voice packet %d: %w", i, ErrIPSCNotConnected)
		}
		if _, err := c.conn.WriteToUDP(p, c.master); err != nil {
			return fmt.Errorf("send voice packet %d: %w", i, err)
		}
	}
	return nil
}

// Close shuts down the client.
func (c *IPSCClient) Close() {
	c.stopOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			c.mu.Lock()
			c.run(c.peer.Close(time.Since(c.start), make([]ipscsession.Output, 0, c.peer.MaxOutputs())))
			c.mu.Unlock()
			_ = c.conn.Close()
		}
	})
	c.wg.Wait()
}

func (c *IPSCClient) run(out []ipscsession.Output) {
	for _, o := range out {
		switch o.Kind {
		case ipscsession.OutputSend:
			to := &net.UDPAddr{IP: net.IPv4(o.Addr.IP[0], o.Addr.IP[1], o.Addr.IP[2], o.Addr.IP[3]), Port: int(o.Addr.Port)}
			_, _ = c.conn.WriteToUDP(o.Packet, to)
		case ipscsession.OutputUp:
			c.upOnce.Do(func() { close(c.ready) })
		case ipscsession.OutputData:
			c.receive(o.Packet)
		case ipscsession.OutputDown, ipscsession.OutputPeers, ipscsession.OutputDropped:
		}
	}
}

func (c *IPSCClient) receive(p []byte) {
	op := ipscwire.Opcode(p[0])
	switch op { //nolint:exhaustive
	case ipscwire.OpGroupVoice, ipscwire.OpPrivateVoice, ipscwire.OpGroupData, ipscwire.OpPrivateData:
	default:
		return
	}
	burst := IPSCBurst{PacketType: byte(op), Data: append([]byte(nil), p...)}
	if u, err := ipscwire.DecodeUser(p); err == nil {
		burst.PeerID = u.ID
		burst.Src = uint(u.Source)
		burst.Dst = uint(u.Destination)
		burst.GroupCall = op == ipscwire.OpGroupVoice || op == ipscwire.OpGroupData
		burst.Slot = u.Info.Slot() == enums.Slot2
	}
	select {
	case c.packets <- burst:
	default:
	}
}

func (c *IPSCClient) rx() {
	defer c.wg.Done()
	buf := make([]byte, ipscsession.MaxPacket)
	for {
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		addr, ok := ipscsession.AddrOf(from.IP, from.Port)
		if !ok {
			continue
		}
		c.mu.Lock()
		c.run(c.peer.Packet(time.Since(c.start), addr, buf[:n], make([]ipscsession.Output, 0, c.peer.MaxOutputs())))
		c.mu.Unlock()
	}
}

func (c *IPSCClient) ticks() {
	defer c.wg.Done()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			c.run(c.peer.Tick(time.Since(c.start), make([]ipscsession.Output, 0, c.peer.MaxOutputs())))
			c.mu.Unlock()
		}
	}
}

// buildVoicePacket constructs an IPSC voice user packet as an XPR 8400
// sends it: a voice LC header or terminator with the Full LC of the call,
// or voice burst A with silence. It returns nil when the packet does not
// encode. The caller holds c.mu.
func (c *IPSCClient) buildVoicePacket(op ipscwire.Opcode, src, dst uint, groupCall, slot bool, streamID uint32, burstType ipscwire.BurstType, isEnd bool) []byte {
	c.rtpSeq++
	seq := c.rtpSeq
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
		_ = u.Info.SetSlot(enums.Slot2)
	}
	if isEnd {
		u.Info.SetEnd(true)
		u.RTP.PayloadType = 0x5E
	}

	var raw layer2.RawBurst
	var err error
	if burstType == ipscwire.BurstVoice {
		raw, err = layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{}, enums.VoiceBurstA, pdu.EmbeddedSignalling{}, [4]byte{}, layer2.SyncSourceMS)
		if err == nil {
			_, err = u.SetRawBurst(raw, layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: enums.VoiceBurstA})
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
			_, err = u.SetRawBurst(raw, layer2.Hint{Kind: layer2.KindData, DataType: dt, HasDataType: true})
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
