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
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscauth"
	ipscsession "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscsession"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

const testHexKey = "0000000000000000000000000000000000001234"

// eventWait bounds the wait for an event of the server, such as a reply or
// a burst. The server drops a peer that it does not hear from in the peer
// timeout of the session, so an event that takes longer has failed.
const eventWait = ipscsession.TimeoutDefault

func await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	timer := time.NewTimer(eventWait)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v
	case <-timer.C:
		t.Fatalf("%s did not come within the peer timeout of %v", what, eventWait)
		var zero T
		return zero
	}
}

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
	return signWith(t, testKey(t), append([]byte(nil), data...))
}

func signWith(t *testing.T, key [20]byte, data []byte) []byte {
	t.Helper()
	out, err := ipscauth.NewKey(key).Sign(data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return out
}

// newTestServerWithUDP returns a server on a loopback socket whose peers
// all have the test key. See newHubServer.
func newTestServerWithUDP(t *testing.T) (*IPSCServer, *net.UDPAddr) {
	t.Helper()
	database, err := db.MakeDB(testDBConfig(t))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	s, addr := newHubServer(t, database)
	key := testKey(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return key, true, nil }
	return s, addr
}

// testDBConfig returns the default config with the network ID of
// testConfig, on an in-memory database.
func testDBConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.DMR.IPSC.NetworkID = testConfig().DMR.IPSC.NetworkID
	memdb.Use(t, &cfg)
	return &cfg
}

// newHubServer returns a server on a loopback socket with a real hub and
// call tracker on database, as production makes it, so that it answers a
// registration once the hub serves the peer. The server does not run its
// reader: a test hands it each packet with handlePacket, as the reader
// does, or runs the reader itself, and may set the burst handler first.
func newHubServer(t *testing.T, database *gorm.DB) (*IPSCServer, *net.UDPAddr) {
	t.Helper()
	cfg := testDBConfig(t)
	cfg.DMR.IPSC.Bind = "127.0.0.1"
	cfg.DMR.IPSC.Port = 0
	kvStore, err := kv.MakeKV(context.Background(), cfg, kv.WithoutCallBound())
	if err != nil {
		t.Fatalf("kv: %v", err)
	}
	ps, err := pubsub.MakePubSub(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pubsub: %v", err)
	}
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	s := NewIPSCServer(cfg, h, database)
	if err := s.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
		ct.Stop()
		_ = ps.Close()
		_ = kvStore.Close()
	})
	addr, ok := s.udp.LocalAddr().(*net.UDPAddr)
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

// readUDP returns the next packet to conn. A peer that hears nothing for
// registerWait drops the server, so a packet that takes longer has failed.
func readUDP(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(registerWait)); err != nil {
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
	p, err := (&ipscwire.Control{Opcode: op, ID: id, Mode: ipscsession.ModeDefault}).AppendBinary(nil)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.m.Peers() {
		if s.m.PeerAt(i).ID == id {
			n++
		}
	}
	return n
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
	dbConn, err := db.MakeDB(testDBConfig(t))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	s, _ := newHubServer(t, dbConn)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1)}
	if err := s.handlePacket(context.Background(), control(t, ipscwire.OpRegisterRequest, 42), addr); err == nil {
		t.Fatal("expected an error for a peer that is not in the DB")
	}
	if peers(s, 42) != 0 {
		t.Fatal("expected no peer for an unknown repeater")
	}
}

func TestDBKeyLookup(t *testing.T) {
	t.Parallel()
	dbConn, err := gorm.Open(sqlite.Open(memdb.DSN()), &gorm.Config{})
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
	if err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpAliveRequest, 3002068)), addr); err != nil {
		t.Fatalf("keepalive: %v", err)
	}
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpAliveReply {
		t.Fatalf("reply opcode 0x%02x, want alive reply", r[0])
	}
	if peers(s, 3002068) != 1 {
		t.Fatal("expected the peer to register")
	}
}

func TestAutoRegisterOnDataFromUnregisteredPeer(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	got := make(chan []byte, 1)
	s.SetBurstHandler(func(_ byte, data []byte, _ *net.UDPAddr, _ time.Time) { got <- data })
	if err := s.handlePacket(context.Background(), signTestPacket(t, voicePacket(3002068)), addr); err != nil {
		t.Fatalf("voice: %v", err)
	}
	if peers(s, 3002068) != 1 {
		t.Fatal("expected the peer to register")
	}
	select {
	case d := <-got:
		if len(d) != 54 {
			t.Fatalf("burst handler got %d octets, want 54 without the trailer", len(d))
		}
	default:
		t.Fatal("the burst handler did not run in handlePacket")
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

func TestAutoRegisterKeylessPeerOnData(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return [20]byte{}, false, nil }
	client, addr := dialServer(t, srv)
	got := make(chan []byte, 1)
	s.SetBurstHandler(func(_ byte, data []byte, _ *net.UDPAddr, _ time.Time) { got <- data })
	if err := s.handlePacket(context.Background(), voicePacket(3002068), addr); err != nil {
		t.Fatalf("voice: %v", err)
	}
	if peers(s, 3002068) != 1 {
		t.Fatal("expected the keyless peer to register")
	}
	select {
	case d := <-got:
		if len(d) != 54 {
			t.Fatalf("burst handler got %d octets, want 54", len(d))
		}
	default:
		t.Fatal("the burst handler did not run in handlePacket")
	}
	expectNoUDP(t, client)
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
	s.tick(context.Background(), s.now()+ipscsession.TimeoutDefault)
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
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1)}
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

func TestSendsAreNotHeld(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	c, a := dialServer(t, srv)
	register(t, s, c, a, 101)

	const n = 40
	data := voicePacket(311860)
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		for range n {
			s.sendPacketInternal(data)
		}
		assert.Zero(t, time.Since(start), "%d sends to one peer waited", n)
	})
	for range n {
		readUDP(t, c)
	}
}

func TestHandlerLoop(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	calls := make(chan struct{}, 2)
	s.SetBurstHandler(func(byte, []byte, *net.UDPAddr, time.Time) { calls <- struct{}{} })
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
	await(t, calls, "the burst")
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if n := len(calls); n != 0 {
		t.Fatalf("burst handler ran %d times, want 1", n+1)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestHandlerLoopKeepsBurstOrder(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	const count = 60
	got := make(chan byte, count)
	s.SetBurstHandler(func(_ byte, data []byte, _ *net.UDPAddr, _ time.Time) { got <- data[53] })
	s.wg.Add(1)
	go s.handler(context.Background())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	client, _ := dialServer(t, srv)
	if _, err := client.Write(signTestPacket(t, control(t, ipscwire.OpRegisterRequest, 78))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if r := readUDP(t, client); ipscwire.Opcode(r[0]) != ipscwire.OpRegisterReply {
		t.Fatalf("reply opcode 0x%02x", r[0])
	}
	for i := range count {
		p := voicePacket(78)
		p[53] = byte(i)
		if _, err := client.Write(signTestPacket(t, p)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i := range count {
		if b := await(t, got, fmt.Sprintf("burst %d of %d", i, count)); b != byte(i) {
			t.Fatalf("burst %d carried index %d", i, b)
		}
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

func TestPeersWithOwnKeys(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	keyA, keyB := testKey(t), [20]byte{19: 0x42}
	s.keyFor = func(id uint32) ([20]byte, bool, error) {
		if id == 201 {
			return keyA, true, nil
		}
		return keyB, true, nil
	}
	cA, aA := dialServer(t, srv)
	cB, aB := dialServer(t, srv)
	for _, p := range []struct {
		c   *net.UDPConn
		a   *net.UDPAddr
		id  uint32
		key [20]byte
	}{{cA, aA, 201, keyA}, {cB, aB, 202, keyB}} {
		req := signWith(t, p.key, control(t, ipscwire.OpRegisterRequest, p.id))
		if err := s.handlePacket(context.Background(), req, p.a); err != nil {
			t.Fatalf("register %d: %v", p.id, err)
		}
		if _, ok := ipscauth.NewKey(p.key).Verify(readUDP(t, p.c)); !ok {
			t.Fatalf("reply to %d is not signed with its key", p.id)
		}
	}
	bad := signWith(t, keyA, control(t, ipscwire.OpAliveRequest, 202))
	if err := s.handlePacket(context.Background(), bad, aB); !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("a packet of 202 signed with the key of 201: err %v", err)
	}
	expectNoUDP(t, cB)

	data := voicePacket(311860)
	s.sendPacketInternal(data)
	for _, p := range []struct {
		c   *net.UDPConn
		key [20]byte
	}{{cA, keyA}, {cB, keyB}} {
		body, ok := ipscauth.NewKey(p.key).Verify(readUDP(t, p.c))
		if !ok || string(body) != string(data) {
			t.Fatal("user packet not signed with the key of the peer")
		}
	}
}

func TestASlowRepeaterTimeWriteDoesNotHoldUpThePackets(t *testing.T) {
	t.Parallel()
	dbConn, err := db.MakeDB(testDBConfig(t))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := dbConn.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 9}, Type: models.RepeaterTypeIPSC}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	handled := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), eventWait)
	t.Cleanup(cancel)
	slow := func(*gorm.DB) {
		select {
		case <-handled:
		case <-ctx.Done():
			t.Error("the registration waited on the database")
		}
	}
	s, srv := newHubServer(t, dbConn)
	s.keyFor = func(uint32) ([20]byte, bool, error) { return [20]byte{}, false, nil }
	if err := dbConn.Callback().Query().Before("gorm:query").Register("test:slow_query", slow); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if err := dbConn.Callback().Update().Before("gorm:update").Register("test:slow_update", slow); err != nil {
		t.Fatalf("callback: %v", err)
	}
	client, addr := dialServer(t, srv)

	before := time.Now()
	err = s.handlePacket(context.Background(), control(t, ipscwire.OpRegisterRequest, 9), addr)
	close(handled)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	readUDP(t, client)

	s.closeTimes()
	var row models.Repeater
	if err := dbConn.First(&row, 9).Error; err != nil {
		t.Fatalf("read the repeater: %v", err)
	}
	if row.Connected.Before(before.Truncate(time.Second)) || row.LastPing.Before(before.Truncate(time.Second)) {
		t.Fatalf("the repeater times are not written: %+v", row)
	}
}
