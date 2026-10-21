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

package ipsc

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/auth"
	"github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/session"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const testHexKey = "0000000000000000000000000000000000001234"

func testConfig() *config.Config {
	return &config.Config{DMR: config.DMR{IPSC: config.IPSC{NetworkID: 311860}}}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex %q: %v", s, err)
	}
	return b
}

func testKey(t *testing.T) [20]byte {
	t.Helper()
	k, err := ipscauth.ParseKey(testHexKey)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	return k
}

func signTestPacket(t *testing.T, data []byte) []byte {
	t.Helper()
	return ipscauth.NewKey(testKey(t)).Sign(append([]byte(nil), data...))
}

// newTestServerWithUDP returns a server on a loopback socket whose peers
// all have the test key.
func newTestServerWithUDP(t *testing.T) (*IPSCServer, *net.UDPAddr) {
	t.Helper()
	s := NewIPSCServer(testConfig(), nil, nil)
	key := testKey(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return key, true, nil }
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.udp = conn
	t.Cleanup(func() {
		s.stopped.Store(true)
		_ = conn.Close()
	})
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	return s, addr
}

func dialServer(t *testing.T, srv *net.UDPAddr) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	c, err := net.DialUDP("udp", nil, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	a, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	return c, a
}

func readUDP(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	return buf[:n]
}

func expectNoUDP(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	if n, _, err := conn.ReadFromUDP(buf); err == nil {
		t.Fatalf("expected no packet, got %x", buf[:n])
	}
}

func control(t *testing.T, op ipscwire.Opcode, id uint32) []byte {
	t.Helper()
	p, err := ipscwire.Control{Opcode: op, ID: id, Mode: session.ModeDefault}.AppendBinary(nil)
	if err != nil {
		t.Fatalf("AppendBinary: %v", err)
	}
	return p
}

func voicePacket(id uint32) []byte {
	p := make([]byte, 54)
	p[0] = byte(ipscwire.OpGroupVoice)
	binary.BigEndian.PutUint32(p[1:5], id)
	return p
}

func peers(s *IPSCServer, id uint32) int {
	s.mu.RLock()
	l, ok := s.links[id]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.Peers()
}

func register(t *testing.T, s *IPSCServer, client *net.UDPConn, addr *net.UDPAddr, id uint32) {
	t.Helper()
	if err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpRegisterRequest, id)), addr); err != nil {
		t.Fatalf("register: %v", err)
	}
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpRegisterReply {
		t.Fatalf("register reply opcode 0x%02x", r[0])
	}
}

func TestUnknownPeerDropped(t *testing.T) {
	t.Parallel()
	dbConn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&models.Repeater{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := NewIPSCServer(testConfig(), nil, dbConn)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	if err := s.handlePacket(context.Background(), control(t, ipscwire.OpRegisterRequest, 42), addr); err == nil {
		t.Fatal("expected an error for a peer that is not in the DB")
	}
	if len(s.links) != 0 {
		t.Fatal("expected no session for an unknown peer")
	}
}

func TestDBKeyLookup(t *testing.T) {
	t.Parallel()
	dbConn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&models.Repeater{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, r := range []models.Repeater{
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 1}, Type: models.RepeaterTypeIPSC, Password: "1234"},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 2}, Type: models.RepeaterTypeIPSC},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 3}, Type: models.RepeaterTypeIPSC, Password: "xyz"},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 4}, Type: models.RepeaterTypeMMDVM, Password: "1234"},
	} {
		if err := dbConn.Create(&r).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	s := NewIPSCServer(testConfig(), nil, dbConn)
	if k, ok, err := s.keyFor(1); err != nil || !ok || k != testKey(t) {
		t.Fatalf("peer 1: key %x auth %v err %v", k, ok, err)
	}
	if _, ok, err := s.keyFor(2); err != nil || ok {
		t.Fatalf("peer 2: auth %v err %v, want no auth", ok, err)
	}
	if _, _, err := s.keyFor(3); !errors.Is(err, ErrInvalidAuthKey) {
		t.Fatalf("peer 3: err %v, want ErrInvalidAuthKey", err)
	}
	if _, _, err := s.keyFor(4); err == nil {
		t.Fatal("peer 4: expected an error for an MMDVM repeater")
	}
}

func TestBadTrailerDropped(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	p := signTestPacket(t, control(t, ipscwire.OpRegisterRequest, 7))
	p[len(p)-1] ^= 0xFF
	if err := s.handlePacket(context.Background(), p, addr); !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("err %v, want ErrPacketIgnored", err)
	}
	expectNoUDP(t, client)
	if peers(s, 7) != 0 {
		t.Fatal("expected the peer not to register")
	}
}

func TestKeylessPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return [20]byte{}, false, nil }
	client, addr := dialServer(t, srv)
	if err := s.handlePacket(context.Background(), control(t, ipscwire.OpRegisterRequest, 8), addr); err != nil {
		t.Fatalf("register: %v", err)
	}
	r := readUDP(t, client)
	c, err := ipscwire.DecodeControl(r)
	if err != nil || len(r) != 16 {
		t.Fatalf("reply %x: %v", r, err)
	}
	if c.Flags.Has(ipscwire.FlagAuth) {
		t.Fatal("a keyless peer should get no auth flag")
	}
}

func TestAutoRegisterOnKeepaliveFromUnregisteredPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	if err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpAliveRequest, 3191868)), addr); err != nil {
		t.Fatalf("keepalive: %v", err)
	}
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpAliveReply {
		t.Fatalf("reply opcode 0x%02x, want alive reply", r[0])
	}
	if peers(s, 3191868) != 1 {
		t.Fatal("expected the peer to register")
	}
}

func TestAutoRegisterOnDataFromUnregisteredPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	got := make(chan []byte, 1)
	s.SetBurstHandler(func(_ byte, data []byte, _ *net.UDPAddr) { got <- data })
	if err := s.handlePacket(context.Background(), signTestPacket(t, voicePacket(3191868)), addr); err != nil {
		t.Fatalf("voice: %v", err)
	}
	if peers(s, 3191868) != 1 {
		t.Fatal("expected the peer to register")
	}
	select {
	case d := <-got:
		if len(d) != 54 {
			t.Fatalf("burst handler got %d octets, want 54 without the trailer", len(d))
		}
	case <-time.After(time.Second):
		t.Fatal("expected the burst handler to run")
	}
	expectNoUDP(t, client)
}

func TestAutoRegisterNeedsGoodTrailer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	_, addr := dialServer(t, srv)
	p := signTestPacket(t, voicePacket(9))
	p[len(p)-1] ^= 0xFF
	if err := s.handlePacket(context.Background(), p, addr); !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("err %v, want ErrPacketIgnored", err)
	}
	if peers(s, 9) != 0 {
		t.Fatal("a packet with a bad trailer must not register its peer")
	}
}

func TestDeregistrationFromUnregisteredPeerIsIgnored(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	_, addr := dialServer(t, srv)
	err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpDeregisterRequest, 5)), addr)
	if !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("err %v, want ErrPacketIgnored", err)
	}
	if peers(s, 5) != 0 {
		t.Fatal("expected the peer to stay unregistered")
	}
}

func TestDeregistrationDropsPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	register(t, s, client, addr, 5)
	if err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpDeregisterRequest, 5)), addr); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	if peers(s, 5) != 0 {
		t.Fatal("expected the peer to be dropped")
	}
}

func TestTimeoutDropsPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	register(t, s, client, addr, 6)
	s.tick(context.Background(), s.now())
	if peers(s, 6) != 1 {
		t.Fatal("expected the peer to stay before the timeout")
	}
	s.tick(context.Background(), s.now()+session.TimeoutDefault)
	if peers(s, 6) != 0 {
		t.Fatal("expected the peer to time out")
	}
}

func TestStoppedIgnoresPackets(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	s.stopped.Store(true)
	for _, p := range [][]byte{
		control(t, ipscwire.OpRegisterRequest, 1),
		control(t, ipscwire.OpAliveRequest, 1),
		voicePacket(1),
	} {
		if err := s.handlePacket(context.Background(), signTestPacket(t, p), addr); !errors.Is(err, ErrPacketIgnored) {
			t.Fatalf("opcode 0x%02x: err %v, want ErrPacketIgnored", p[0], err)
		}
	}
	expectNoUDP(t, client)
	if peers(s, 1) != 0 {
		t.Fatal("expected no registration while stopped")
	}
}

func TestShortPacket(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	for _, p := range [][]byte{nil, {0x90}, {0x90, 0, 0, 0}} {
		if err := s.handlePacket(context.Background(), p, addr); err == nil {
			t.Fatalf("expected an error for %x", p)
		}
	}
}

func TestSendToRegisteredPeers(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	c1, a1 := dialServer(t, srv)
	c2, a2 := dialServer(t, srv)
	c3, a3 := dialServer(t, srv)
	register(t, s, c1, a1, 101)
	register(t, s, c2, a2, 102)
	if err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpDeregisterRequest, 103)), a3); !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("err %v", err)
	}

	data := voicePacket(311860)
	s.sendPacketInternal(data)
	for _, c := range []*net.UDPConn{c1, c2} {
		got := readUDP(t, c)
		body, ok := ipscauth.NewKey(testKey(t)).Verify(got)
		if !ok || string(body) != string(data) {
			t.Fatalf("got %x, want %x signed", got, data)
		}
	}
	expectNoUDP(t, c3)

	s.stopped.Store(true)
	s.sendPacketInternal(data)
	expectNoUDP(t, c1)
}

func TestPace(t *testing.T) {
	t.Parallel()
	var l link
	l.pace()
	start := time.Now()
	l.pace()
	if d := time.Since(start); d < burstInterval-time.Millisecond {
		t.Fatalf("second send after %v, want at least %v", d, burstInterval)
	}
}

func TestHandlerLoop(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	var calls atomic.Int32
	s.SetBurstHandler(func(byte, []byte, *net.UDPAddr) { calls.Add(1) })
	s.wg.Add(1)
	go s.handler(context.Background())
	client, _ := dialServer(t, srv)
	if _, err := client.Write(signTestPacket(t, control(t, ipscwire.OpRegisterRequest, 77))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpRegisterReply {
		t.Fatalf("reply opcode 0x%02x", r[0])
	}
	if _, err := client.Write(signTestPacket(t, voicePacket(77))); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("burst handler ran %d times, want 1", calls.Load())
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func FuzzHandlePacket(f *testing.F) {
	f.Add([]byte{0x90, 0x00, 0x04, 0xC2, 0x34})
	f.Add([]byte{})
	f.Add(voicePacket(1))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, srv := newTestServerWithUDP(t)
		_, addr := dialServer(t, srv)
		_ = s.handlePacket(context.Background(), data, addr)
		_ = s.handlePacket(context.Background(), signTestPacket(t, data), addr)
	})
}
