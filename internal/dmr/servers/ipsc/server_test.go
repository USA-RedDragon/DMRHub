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
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testConfig() *config.Config {
	return &config.Config{
		DMR: config.DMR{
			IPSC: config.IPSC{
				NetworkID: 311860,
			},
		},
	}
}

func TestParsePeerID(t *testing.T) {
	t.Parallel()
	data := make([]byte, 5)
	binary.BigEndian.PutUint32(data[1:5], 12345)
	id, err := parsePeerID(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 12345 {
		t.Fatalf("expected 12345, got %d", id)
	}
}

func TestParsePeerIDTooShort(t *testing.T) {
	t.Parallel()
	_, err := parsePeerID([]byte{0x90, 0x00})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestParsePeerIDMaxValue(t *testing.T) {
	t.Parallel()
	data := []byte{0x90, 0xFF, 0xFF, 0xFF, 0xFF}
	id, err := parsePeerID(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 0xFFFFFFFF {
		t.Fatalf("expected 0xFFFFFFFF, got %d", id)
	}
}

func TestUint16ToBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val    uint16
		expect [2]byte
	}{
		{0, [2]byte{0, 0}},
		{1, [2]byte{0, 1}},
		{0xFF00, [2]byte{0xFF, 0x00}},
		{0xBEEF, [2]byte{0xBE, 0xEF}},
	}
	for _, tt := range tests {
		b := uint16ToBytes(tt.val)
		if b[0] != tt.expect[0] || b[1] != tt.expect[1] {
			t.Errorf("uint16ToBytes(%d) = %v, want %v", tt.val, b, tt.expect)
		}
	}
}

func TestUint32ToBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val    uint32
		expect [4]byte
	}{
		{0, [4]byte{0, 0, 0, 0}},
		{1, [4]byte{0, 0, 0, 1}},
		{0xDEADBEEF, [4]byte{0xDE, 0xAD, 0xBE, 0xEF}},
	}
	for _, tt := range tests {
		b := uint32ToBytes(tt.val)
		if b[0] != tt.expect[0] || b[1] != tt.expect[1] || b[2] != tt.expect[2] || b[3] != tt.expect[3] {
			t.Errorf("uint32ToBytes(%d) = %v, want %v", tt.val, b, tt.expect)
		}
	}
}

func TestCloneUDPAddrNil(t *testing.T) {
	t.Parallel()
	if cloneUDPAddr(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestCloneUDPAddr(t *testing.T) {
	t.Parallel()
	orig := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234, Zone: "eth0"}
	clone := cloneUDPAddr(orig)

	if !clone.IP.Equal(orig.IP) || clone.Port != orig.Port || clone.Zone != orig.Zone {
		t.Fatalf("clone mismatch: orig=%v clone=%v", orig, clone)
	}

	// Mutating the clone must not affect the original.
	clone.IP[0] = 99
	if orig.IP[0] == 99 {
		t.Fatal("clone shares underlying IP slice with original")
	}
}

func TestCloneUDPAddrNilIP(t *testing.T) {
	t.Parallel()
	orig := &net.UDPAddr{Port: 5678}
	clone := cloneUDPAddr(orig)
	if clone.IP != nil {
		t.Fatalf("expected nil IP, got %v", clone.IP)
	}
	if clone.Port != 5678 {
		t.Fatalf("expected port 5678, got %d", clone.Port)
	}
}

func TestAuth(t *testing.T) {
	t.Parallel()
	hexKey := "0000000000000000000000000000000000001234"
	authKey := decodeAuthKey("1234")

	payload := []byte("hello world")
	h := hmac.New(sha1.New, mustDecodeHex(t, hexKey))
	h.Write(payload)
	hash := h.Sum(nil)[:10]
	data := make([]byte, 0, len(payload)+len(hash))
	data = append(data, payload...)
	data = append(data, hash...)

	if !authWithKey(data, authKey) {
		t.Fatal("expected auth to pass")
	}
}

func TestAuthBadHash(t *testing.T) {
	t.Parallel()
	authKey := decodeAuthKey("1234")

	payload := []byte("hello world")
	bad := make([]byte, 10)
	data := make([]byte, 0, len(payload)+len(bad))
	data = append(data, payload...)
	data = append(data, bad...)

	if authWithKey(data, authKey) {
		t.Fatal("expected auth to fail with bad hash")
	}
}

func mustDecodeHex(t *testing.T, hexStr string) []byte {
	t.Helper()
	b := make([]byte, len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		var val byte
		for j := 0; j < 2; j++ {
			c := hexStr[i+j]
			switch {
			case c >= '0' && c <= '9':
				val = val*16 + (c - '0')
			case c >= 'a' && c <= 'f':
				val = val*16 + (c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				val = val*16 + (c - 'A' + 10)
			}
		}
		b[i/2] = val
	}
	return b
}

func newTestServer(t *testing.T, cfg *config.Config) *IPSCServer {
	t.Helper()
	s := NewIPSCServer(cfg, nil, nil)
	if s == nil {
		t.Fatal("NewIPSCServer returned nil")
	}
	return s
}

func TestNewIPSCServerNoAuth(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.localID != cfg.DMR.IPSC.NetworkID {
		t.Fatalf("expected localID %d, got %d", cfg.DMR.IPSC.NetworkID, s.localID)
	}
}

func TestNewIPSCServerWithAuth(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestDecodeAuthKey(t *testing.T) {
	t.Parallel()
	key := decodeAuthKey("ABCD")
	if key == nil {
		t.Fatal("expected non-nil auth key")
	}
	if len(key) != 20 {
		t.Fatalf("expected 20-byte auth key, got %d", len(key))
	}
}

func TestDefaultModeByte(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	mode := s.defaultModeByte()
	// Should have operational, digital, ts1, ts2 bits
	if mode&0b01000000 == 0 {
		t.Fatal("expected peerOperational bit set")
	}
	if mode&0b00100000 == 0 {
		t.Fatal("expected peerDigital bit set")
	}
	if mode&0b00001000 == 0 {
		t.Fatal("expected ts1On bit set")
	}
	if mode&0b00000010 == 0 {
		t.Fatal("expected ts2On bit set")
	}
}

func TestDefaultFlagsBytes(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	flags := s.defaultFlagsBytes()
	if flags[3]&0x10 == 0 {
		t.Fatal("expected auth flag always set")
	}
	if flags[3]&0x0D != 0x0D {
		t.Fatalf("expected base flags 0x0D, got %02X", flags[3])
	}
}

func TestDefaultFlagsBytesWithAuth(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	flags := s.defaultFlagsBytes()
	if flags[3]&0x10 == 0 {
		t.Fatal("expected auth flag set when auth enabled")
	}
}

func TestBuildMasterRegisterReply(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	reply := s.buildMasterRegisterReply()

	if reply[0] != byte(PacketType_MasterRegisterReply) {
		t.Fatalf("expected packet type 0x%02X, got 0x%02X", PacketType_MasterRegisterReply, reply[0])
	}

	id := binary.BigEndian.Uint32(reply[1:5])
	if id != cfg.DMR.IPSC.NetworkID {
		t.Fatalf("expected ID %d, got %d", cfg.DMR.IPSC.NetworkID, id)
	}
}

func TestBuildMasterAliveReply(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	reply := s.buildMasterAliveReply()

	if reply[0] != byte(PacketType_MasterAliveReply) {
		t.Fatalf("expected packet type 0x%02X, got 0x%02X", PacketType_MasterAliveReply, reply[0])
	}
}

func TestBuildPeerListReplyEmpty(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	reply := s.buildPeerListReply()

	if reply[0] != byte(PacketType_PeerListReply) {
		t.Fatalf("expected packet type 0x%02X, got 0x%02X", PacketType_PeerListReply, reply[0])
	}

	// Peer count should be 0
	peerCount := binary.BigEndian.Uint16(reply[5:7])
	if peerCount != 0 {
		t.Fatalf("expected 0 peers, got %d", peerCount)
	}
}

func TestBuildPeerListReplyWithPeers(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 50000}
	s.upsertPeer(context.Background(), 42, addr, 0x6A, [4]byte{0, 0, 0, 0x0D})

	reply := s.buildPeerListReply()
	if reply[0] != byte(PacketType_PeerListReply) {
		t.Fatalf("expected packet type 0x%02X, got 0x%02X", PacketType_PeerListReply, reply[0])
	}

	// Should have at least the peer entry bytes after the header
	if len(reply) < 7+11 {
		t.Fatalf("reply too short for 1 peer: %d bytes", len(reply))
	}
}

func TestUpsertPeerAndCount(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	if s.peerCount() != 0 {
		t.Fatalf("expected 0 peers initially, got %d", s.peerCount())
	}

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	s.upsertPeer(context.Background(), 100, addr, 0x6A, [4]byte{})

	if s.peerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", s.peerCount())
	}

	// Upsert same peer should not increase count
	s.upsertPeer(context.Background(), 100, addr, 0x6A, [4]byte{})
	if s.peerCount() != 1 {
		t.Fatalf("expected still 1 peer after upsert, got %d", s.peerCount())
	}

	// Add a different peer
	addr2 := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 5678}
	s.upsertPeer(context.Background(), 200, addr2, 0x6A, [4]byte{})
	if s.peerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", s.peerCount())
	}
}

func TestMarkPeerAlive(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	s.markPeerAlive(100, addr)

	if s.peerCount() != 1 {
		t.Fatalf("expected 1 peer after markPeerAlive, got %d", s.peerCount())
	}

	// Mark alive again should increment keepalive counter
	s.markPeerAlive(100, addr)
	s.mu.RLock()
	peer := s.peers[100]
	keepAlive := peer.KeepAliveReceived
	s.mu.RUnlock()

	if keepAlive != 2 {
		t.Fatalf("expected 2 keepalives, got %d", keepAlive)
	}
}

func TestHandlePacketTooShort(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	_, err := s.handlePacket(context.Background(), []byte{}, addr)
	if err == nil {
		t.Fatal("expected error on empty packet")
	}
}

func TestHandlePacketUnknownType(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	data := []byte{0xFF, 0, 0, 0, 1}
	_, err := s.handlePacket(context.Background(), data, addr)
	if err == nil {
		t.Fatal("expected error for unknown packet type 0xFF")
	}
}

func TestHandlePacketReplyTypesIgnored(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	peerID := uint32(12345)
	preRegisterTestPeer(s, peerID)

	replyTypes := []byte{
		byte(PacketType_MasterRegisterReply),
		byte(PacketType_PeerListReply),
		byte(PacketType_MasterAliveReply),
	}
	for _, pt := range replyTypes {
		data := make([]byte, 5)
		data[0] = pt
		binary.BigEndian.PutUint32(data[1:5], peerID)
		signed := signTestPacket(t, data)
		_, err := s.handlePacket(context.Background(), signed, addr)
		if !errors.Is(err, ErrPacketIgnored) {
			t.Fatalf("expected ErrPacketIgnored for type 0x%02X, got %v", pt, err)
		}
	}
}

func TestPacketTypeValues(t *testing.T) {
	t.Parallel()
	// Verify the packet type constants match the IPSC protocol
	expected := map[PacketType]byte{
		PacketType_GroupVoice:            0x80,
		PacketType_PrivateVoice:          0x81,
		PacketType_GroupData:             0x83,
		PacketType_PrivateData:           0x84,
		PacketType_RepeaterWakeUp:        0x85,
		PacketType_MasterRegisterRequest: 0x90,
		PacketType_MasterRegisterReply:   0x91,
		PacketType_PeerListRequest:       0x92,
		PacketType_PeerListReply:         0x93,
		PacketType_MasterAliveRequest:    0x96,
		PacketType_MasterAliveReply:      0x97,
	}
	for pt, val := range expected {
		if byte(pt) != val {
			t.Errorf("PacketType %v: expected 0x%02X, got 0x%02X", pt, val, byte(pt))
		}
	}
}

func TestUpsertPeerRegistrationStatus(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	s.upsertPeer(context.Background(), 100, addr, 0x6A, [4]byte{})

	s.mu.RLock()
	peer := s.peers[100]
	registered := peer.RegistrationStatus
	lastSeen := peer.LastSeen
	s.mu.RUnlock()

	if !registered {
		t.Fatal("expected peer to be registered")
	}
	if time.Since(lastSeen) > time.Second {
		t.Fatal("expected LastSeen to be recent")
	}
}

// --- Helper: create a server with a real loopback UDP socket ---

func newTestServerWithUDP(t *testing.T) (*IPSCServer, *net.UDPAddr) {
	t.Helper()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// Bind to loopback on a random port
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
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

func newTestServerWithUDPAndDB(t *testing.T) (*IPSCServer, *net.UDPAddr) {
	t.Helper()
	dbConn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := dbConn.AutoMigrate(&models.Repeater{}); err != nil {
		t.Fatalf("failed to migrate repeater model: %v", err)
	}

	s, addr := newTestServerWithUDP(t)
	s.db = dbConn

	sqlDB, err := dbConn.DB()
	if err == nil {
		t.Cleanup(func() {
			_ = sqlDB.Close()
		})
	}

	return s, addr
}

// readUDP reads one datagram from the given conn with a timeout.
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

// makeControlPacket builds a minimal control packet (type + 4-byte peer ID).
func makeControlPacket(packetType PacketType, peerID uint32) []byte {
	data := make([]byte, 5)
	data[0] = byte(packetType)
	binary.BigEndian.PutUint32(data[1:5], peerID)
	return data
}

// makeControlPacketWithModeFlags builds a control packet with mode + flags.
func makeControlPacketWithModeFlags(packetType PacketType, peerID uint32, mode byte, flags [4]byte) []byte {
	data := make([]byte, 10)
	data[0] = byte(packetType)
	binary.BigEndian.PutUint32(data[1:5], peerID)
	data[5] = mode
	copy(data[6:10], flags[:])
	return data
}

// signPacket appends an HMAC-SHA1 hash to the packet data.
func signPacket(t *testing.T, data []byte, hexKey string) []byte {
	t.Helper()
	key := mustDecodeHex(t, hexKey)
	h := hmac.New(sha1.New, key)
	h.Write(data)
	hash := h.Sum(nil)[:10]
	return append(data, hash...)
}

const testHexKey = "0000000000000000000000000000000000001234"

func testAuthKey() []byte {
	return decodeAuthKey("1234")
}

// preRegisterTestPeer adds a peer with the standard test auth key to the server.
func preRegisterTestPeer(s *IPSCServer, peerID uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[peerID]
	if !ok {
		peer = &Peer{ID: peerID}
		s.peers[peerID] = peer
	}
	peer.AuthKey = testAuthKey()
	peer.RegistrationStatus = true
}

// signTestPacket signs a packet with the standard test auth key.
func signTestPacket(t *testing.T, data []byte) []byte {
	t.Helper()
	return signPacket(t, data, testHexKey)
}

// --- sendPacket tests ---

func TestSendPacketNoAuth(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	// Create a client UDP socket to receive the packet
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()
	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	// Send a packet to the client
	_ = srvAddr // server address, not needed directly
	payload := []byte("hello")
	pkt := &Packet{data: payload}
	if err := s.sendPacket(pkt, clientAddr, nil); err != nil {
		t.Fatalf("sendPacket error: %v", err)
	}

	got := readUDP(t, client)
	if string(got) != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestSendPacketWithAuth(t *testing.T) {
	t.Parallel()
	hexKey := "0000000000000000000000000000000000001234"
	s, _ := newTestServerWithUDP(t)

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()
	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	payload := []byte("secure")
	pkt := &Packet{data: payload}
	if err := s.sendPacket(pkt, clientAddr, decodeAuthKey("1234")); err != nil {
		t.Fatalf("sendPacket error: %v", err)
	}

	got := readUDP(t, client)
	// Should be payload + 10-byte HMAC
	if len(got) != len(payload)+10 {
		t.Fatalf("expected %d bytes (payload+hash), got %d", len(payload)+10, len(got))
	}

	// Verify the HMAC
	authKey := mustDecodeHex(t, hexKey)
	h := hmac.New(sha1.New, authKey)
	h.Write(payload)
	expectedHash := h.Sum(nil)[:10]
	if !hmac.Equal(got[len(payload):], expectedHash) {
		t.Fatal("HMAC mismatch on sent packet")
	}
}

// --- Handler flows with real UDP ---

func TestHandleMasterRegisterRequestFlow(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Build a register request with mode and flags
	peerID := uint32(55555)
	preRegisterTestPeer(s, peerID)
	reqData := signTestPacket(t, makeControlPacketWithModeFlags(PacketType_MasterRegisterRequest, peerID, 0x6A, [4]byte{0, 0, 0, 0x0D}))
	clientUDPAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), reqData, clientUDPAddr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	// Verify peer was registered
	s.mu.RLock()
	peer, ok := s.peers[peerID]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("expected peer to be registered")
	}
	if peer.Mode != 0x6A {
		t.Fatalf("expected mode 0x6A, got 0x%02X", peer.Mode)
	}
	if !peer.RegistrationStatus {
		t.Fatal("expected peer registered=true")
	}

	// Verify reply was sent
	reply := readUDP(t, client)
	if reply[0] != byte(PacketType_MasterRegisterReply) {
		t.Fatalf("expected register reply type 0x%02X, got 0x%02X", PacketType_MasterRegisterReply, reply[0])
	}
}

func TestHandleMasterRegisterRequestShortPacket(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Short register request (no mode/flags) — should still work using defaults
	preRegisterTestPeer(s, 77777)
	reqData := signTestPacket(t, makeControlPacket(PacketType_MasterRegisterRequest, 77777))
	clientUDPAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), reqData, clientUDPAddr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	s.mu.RLock()
	peer := s.peers[77777]
	s.mu.RUnlock()
	if peer == nil {
		t.Fatal("expected peer to exist")
	}
	// Mode should be the default
	if peer.Mode != s.defaultModeByte() {
		t.Fatalf("expected default mode, got 0x%02X", peer.Mode)
	}
}

func TestHandleMasterAliveRequestFlow(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	peerID := uint32(66666)
	preRegisterTestPeer(s, peerID)
	reqData := signTestPacket(t, makeControlPacket(PacketType_MasterAliveRequest, peerID))
	aliveAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), reqData, aliveAddr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	// Verify peer was marked alive
	s.mu.RLock()
	peer := s.peers[peerID]
	s.mu.RUnlock()
	if peer == nil {
		t.Fatal("expected peer to exist after alive request")
	}
	if peer.KeepAliveReceived != 1 {
		t.Fatalf("expected 1 keepalive, got %d", peer.KeepAliveReceived)
	}

	// Verify reply was sent
	reply := readUDP(t, client)
	if reply[0] != byte(PacketType_MasterAliveReply) {
		t.Fatalf("expected alive reply type 0x%02X, got 0x%02X", PacketType_MasterAliveReply, reply[0])
	}
}

func TestHandlePeerListRequestFlow(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	preRegisterTestPeer(s, 88888)
	reqData := signTestPacket(t, makeControlPacket(PacketType_PeerListRequest, 88888))
	peerListAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), reqData, peerListAddr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	reply := readUDP(t, client)
	if reply[0] != byte(PacketType_PeerListReply) {
		t.Fatalf("expected peer list reply type 0x%02X, got 0x%02X", PacketType_PeerListReply, reply[0])
	}
}

func TestHandlePeerListRequestTooShort(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Packet type + only 2 bytes (too short for peer ID)
	data := []byte{byte(PacketType_PeerListRequest), 0x00, 0x01}
	shortAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), data, shortAddr)
	if err == nil {
		t.Fatal("expected error for too-short peer list request")
	}
}

func TestHandleRepeaterWakeUp(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 3000}
	peerID := uint32(11111)
	preRegisterTestPeer(s, peerID)
	data := signTestPacket(t, makeControlPacket(PacketType_RepeaterWakeUp, peerID))

	_, err := s.handlePacket(context.Background(), data, addr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	s.mu.RLock()
	peer := s.peers[peerID]
	s.mu.RUnlock()
	if peer == nil {
		t.Fatal("expected peer to exist after wake-up")
	}
	if peer.KeepAliveReceived != 1 {
		t.Fatalf("expected 1 keepalive, got %d", peer.KeepAliveReceived)
	}
}

func TestHandleRepeaterWakeUpTooShort(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 3000}
	data := []byte{byte(PacketType_RepeaterWakeUp), 0x00}
	_, err := s.handlePacket(context.Background(), data, addr)
	if err == nil {
		t.Fatal("expected error for too-short wake-up packet")
	}
}

// --- User packet and burst handler tests ---

func TestHandleUserPacketCallsBurstHandler(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	var received atomic.Bool
	var gotType atomic.Uint32
	var wg sync.WaitGroup
	wg.Add(1)

	s.SetBurstHandler(func(packetType byte, data []byte, addr *net.UDPAddr) {
		defer wg.Done()
		received.Store(true)
		gotType.Store(uint32(packetType))
	})

	peerID := uint32(42)
	preRegisterTestPeer(s, peerID)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	data := make([]byte, 54)
	data[0] = byte(PacketType_GroupVoice)
	binary.BigEndian.PutUint32(data[1:5], peerID)

	_, err := s.handlePacket(context.Background(), signTestPacket(t, data), addr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	wg.Wait()
	if !received.Load() {
		t.Fatal("burst handler was not called")
	}
	if gotType.Load() != uint32(PacketType_GroupVoice) {
		t.Fatalf("expected packet type 0x80, got 0x%02X", gotType.Load())
	}

	// Verify peer was marked alive
	s.mu.RLock()
	peer := s.peers[42]
	s.mu.RUnlock()
	if peer == nil {
		t.Fatal("expected peer to be created")
	}
}

func TestHandleUserPacketNoBurstHandler(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	// No burst handler set

	peerID := uint32(43)
	preRegisterTestPeer(s, peerID)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	data := make([]byte, 54)
	data[0] = byte(PacketType_PrivateVoice)
	binary.BigEndian.PutUint32(data[1:5], peerID)

	_, err := s.handlePacket(context.Background(), signTestPacket(t, data), addr)
	if err != nil {
		t.Fatalf("handlePacket error (no handler): %v", err)
	}
}

func TestHandleUserPacketAllTypes(t *testing.T) {
	t.Parallel()

	types := []PacketType{
		PacketType_GroupVoice,
		PacketType_PrivateVoice,
		PacketType_GroupData,
		PacketType_PrivateData,
	}

	for _, pt := range types {
		cfg := testConfig()
		s := newTestServer(t, cfg)

		peerID := uint32(50)
		preRegisterTestPeer(s, peerID)
		addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
		data := make([]byte, 54)
		data[0] = byte(pt)
		binary.BigEndian.PutUint32(data[1:5], peerID)

		_, err := s.handlePacket(context.Background(), signTestPacket(t, data), addr)
		if err != nil {
			t.Fatalf("handlePacket for type 0x%02X error: %v", byte(pt), err)
		}
	}
}

func TestHandleUserPacketTooShortForPeerID(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	data := []byte{byte(PacketType_GroupVoice), 0x00, 0x01}

	_, err := s.handlePacket(context.Background(), data, addr)
	if err == nil {
		t.Fatal("expected error for user packet too short for peer ID")
	}
}

func TestHandleUserPacketBurstHandlerReceivesDataCopy(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	var receivedData []byte
	var wg sync.WaitGroup
	wg.Add(1)

	s.SetBurstHandler(func(packetType byte, data []byte, addr *net.UDPAddr) {
		defer wg.Done()
		receivedData = data
	})

	peerID := uint32(42)
	preRegisterTestPeer(s, peerID)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	data := make([]byte, 20)
	data[0] = byte(PacketType_GroupVoice)
	binary.BigEndian.PutUint32(data[1:5], peerID)
	data[10] = 0xAA // sentinel value

	signedData := signTestPacket(t, data)
	_, err := s.handlePacket(context.Background(), signedData, addr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	wg.Wait()

	// Mutate original to verify handler got a copy
	data[10] = 0xFF
	if receivedData[10] != 0xAA {
		t.Fatal("burst handler should receive a copy of the data, not a reference")
	}
}

// --- Auth-related handlePacket tests ---

func TestHandlePacketAuthEnabledTooShort(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	// A packet with auth enabled but only 10 bytes (not enough for payload + hash)
	data := make([]byte, 10)
	data[0] = byte(PacketType_MasterRegisterRequest)
	_, err := s.handlePacket(context.Background(), data, addr)
	if err == nil {
		t.Fatal("expected error for auth-enabled packet too short")
	}
}

func TestHandlePacketAuthEnabledBadHash(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	// Pre-register peer with auth key so we can test bad hash path
	s.mu.Lock()
	s.peers[12345] = &Peer{ID: 12345, AuthKey: decodeAuthKey("1234")}
	s.mu.Unlock()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	// Valid-length packet but bad hash
	payload := makeControlPacket(PacketType_MasterRegisterRequest, 12345)
	payload = append(payload, make([]byte, 10)...) // 10 zero bytes as bad hash
	data := payload
	_, err := s.handlePacket(context.Background(), data, addr)
	if err == nil {
		t.Fatal("expected auth failure error")
	}
}

func TestHandlePacketAuthEnabledSuccess(t *testing.T) {
	t.Parallel()
	hexKey := "0000000000000000000000000000000000001234"
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	peerID := uint32(99999)
	// Pre-register peer with auth key for per-peer auth
	s.mu.Lock()
	s.peers[peerID] = &Peer{ID: peerID, AuthKey: decodeAuthKey("1234")}
	s.mu.Unlock()

	payload := makeControlPacket(PacketType_MasterRegisterRequest, peerID)
	data := signPacket(t, payload, hexKey)

	authAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), data, authAddr)
	if err != nil {
		t.Fatalf("expected auth success, got error: %v", err)
	}

	// Verify peer was registered
	s.mu.RLock()
	_, ok = s.peers[peerID]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("expected peer to be registered after authenticated request")
	}
}

// --- sendPacketInternal tests ---

func TestSendUserPacketToMultiplePeers(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)

	// Create two client sockets to receive
	client1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client1 listen: %v", err)
	}
	defer func() { _ = client1.Close() }()

	client2, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client2 listen: %v", err)
	}
	defer func() { _ = client2.Close() }()

	// Register two peers
	client1Addr, ok := client1.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	client2Addr, ok := client2.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	s.upsertPeer(context.Background(), 1, client1Addr, 0x6A, [4]byte{})
	s.upsertPeer(context.Background(), 2, client2Addr, 0x6A, [4]byte{})

	payload := []byte("broadcast")
	s.sendPacketInternal(payload)

	got1 := readUDP(t, client1)
	got2 := readUDP(t, client2)

	if string(got1) != "broadcast" {
		t.Fatalf("client1 expected 'broadcast', got %q", got1)
	}
	if string(got2) != "broadcast" {
		t.Fatalf("client2 expected 'broadcast', got %q", got2)
	}
}

func TestSendUserPacketWhenStopped(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()

	stoppedAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	s.upsertPeer(context.Background(), 1, stoppedAddr, 0x6A, [4]byte{})
	s.stopped.Store(true)

	s.sendPacketInternal([]byte("should not arrive"))

	// Try reading with a short timeout — should get nothing
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	_, _, err = client.ReadFromUDP(buf)
	if err == nil {
		t.Fatal("expected no data when server is stopped")
	}
}

func TestSendUserPacketNoPeers(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)

	// Should not panic with no peers
	s.sendPacketInternal([]byte("no peers"))
}

func TestSendUserPacketSkipsNilAddrPeers(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Add a peer with nil addr
	s.mu.Lock()
	s.peers[999] = &Peer{ID: 999, Addr: nil}
	s.mu.Unlock()

	// Add a peer with a real addr
	clientAddrSelective, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	s.upsertPeer(context.Background(), 1, clientAddrSelective, 0x6A, [4]byte{})

	s.sendPacketInternal([]byte("selective"))

	got := readUDP(t, client)
	if string(got) != "selective" {
		t.Fatalf("expected 'selective', got %q", got)
	}
}

func TestSendUserPacketWithAuth(t *testing.T) {
	t.Parallel()
	hexKey := "0000000000000000000000000000000000005678"
	s, _ := newTestServerWithUDP(t)

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()

	clientAddrAuth, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	s.upsertPeer(context.Background(), 1, clientAddrAuth, 0x6A, [4]byte{})
	// Set auth key for the peer since auth is now per-peer
	s.mu.Lock()
	s.peers[1].AuthKey = decodeAuthKey("5678")
	s.mu.Unlock()

	payload := []byte("authenticated-broadcast")
	s.sendPacketInternal(payload)

	got := readUDP(t, client)
	if len(got) != len(payload)+10 {
		t.Fatalf("expected %d bytes, got %d", len(payload)+10, len(got))
	}

	// Verify HMAC on received data
	authKey := mustDecodeHex(t, hexKey)
	h := hmac.New(sha1.New, authKey)
	h.Write(got[:len(got)-10])
	if !hmac.Equal(got[len(got)-10:], h.Sum(nil)[:10]) {
		t.Fatal("HMAC mismatch on broadcast packet")
	}
}

func TestSendUserPacketDataIsCopied(t *testing.T) {
	t.Parallel()
	s, _ := newTestServerWithUDP(t)

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()

	copyAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	s.upsertPeer(context.Background(), 1, copyAddr, 0x6A, [4]byte{})

	payload := []byte("original")
	s.sendPacketInternal(payload)

	// Mutate original after sending — should not affect what was sent
	payload[0] = 'X'

	got := readUDP(t, client)
	if got[0] != 'o' {
		t.Fatal("sendPacketInternal should copy data, not use original slice")
	}
}

// --- pacePeer tests ---

func TestPacePeerFirstCallNoDelay(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	start := time.Now()
	s.pacePeer(1)
	elapsed := time.Since(start)

	// First call should not sleep
	if elapsed > 10*time.Millisecond {
		t.Fatalf("first pacePeer took too long: %v", elapsed)
	}
}

func TestPacePeerEnforcesInterval(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// First call establishes the timestamp
	s.pacePeer(1)

	// Second call immediately after should sleep ~30ms
	start := time.Now()
	s.pacePeer(1)
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Fatalf("expected pacePeer to sleep ~30ms, only waited %v", elapsed)
	}
}

func TestPacePeerSeparatePeersIndependent(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// Pace peer 1
	s.pacePeer(1)

	// Pace peer 2 immediately — should not be delayed by peer 1
	start := time.Now()
	s.pacePeer(2)
	elapsed := time.Since(start)

	if elapsed > 10*time.Millisecond {
		t.Fatalf("pacing different peer shouldn't delay, took %v", elapsed)
	}
}

func TestPacePeerAfterInterval(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	s.pacePeer(1)
	// Wait longer than the burst interval (30ms)
	time.Sleep(40 * time.Millisecond)

	start := time.Now()
	s.pacePeer(1)
	elapsed := time.Since(start)

	// Should not need to sleep since interval has passed
	if elapsed > 10*time.Millisecond {
		t.Fatalf("pacePeer should not sleep after interval passed, took %v", elapsed)
	}
}

// --- handler() loop and Stop() tests ---

func TestHandlerLoopProcessesPackets(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// Bind to loopback
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.udp = conn
	srvAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	// Start the handler goroutine
	s.wg.Add(1)
	go s.handler(context.Background())

	// Send a wake-up packet to the server
	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	peerID := uint32(22222)
	preRegisterTestPeer(s, peerID)
	data := signTestPacket(t, makeControlPacket(PacketType_RepeaterWakeUp, peerID))
	if _, err := client.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Poll for peer to appear (the handler processes asynchronously)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		_, ok := s.peers[peerID]
		s.mu.RUnlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.RLock()
	_, found := s.peers[peerID]
	s.mu.RUnlock()
	if !found {
		t.Fatal("expected peer to be created by handler loop")
	}

	// Stop cleanly
	s.stopped.Store(true)
	_ = conn.Close()
	s.wg.Wait()
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.udp = conn

	s.wg.Add(1)
	go s.handler(context.Background())

	// Calling Stop multiple times should not panic
	_ = s.Stop(context.Background())
	_ = s.Stop(context.Background())
	_ = s.Stop(context.Background())
}

func TestStopWithNilConn(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	// udp is nil — Stop should not panic
	_ = s.Stop(context.Background())
}

func TestHandlerLoopWithBurstHandler(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.udp = conn
	srvAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	var received atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	s.SetBurstHandler(func(packetType byte, data []byte, addr *net.UDPAddr) {
		defer wg.Done()
		received.Store(true)
	})

	s.wg.Add(1)
	go s.handler(context.Background())

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	peerID := uint32(33333)
	preRegisterTestPeer(s, peerID)
	data := make([]byte, 54)
	data[0] = byte(PacketType_GroupVoice)
	binary.BigEndian.PutUint32(data[1:5], peerID)

	if _, err := client.Write(signTestPacket(t, data)); err != nil {
		t.Fatalf("write: %v", err)
	}

	wg.Wait()
	if !received.Load() {
		t.Fatal("burst handler was not called via handler loop")
	}

	s.stopped.Store(true)
	_ = conn.Close()
	s.wg.Wait()
}

// --- buildPeerList edge cases ---

func TestBuildPeerListSkipsNilAddr(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	s.mu.Lock()
	s.peers[1] = &Peer{ID: 1, Addr: nil, Mode: 0x6A}
	s.peers[2] = &Peer{ID: 2, Addr: &net.UDPAddr{IP: nil, Port: 1234}, Mode: 0x6A}
	s.mu.Unlock()

	peerList := s.buildPeerList()
	// Both peers should be skipped (nil Addr, nil IP)
	if len(peerList) != 0 {
		t.Fatalf("expected empty peer list, got %d bytes", len(peerList))
	}
}

func TestBuildPeerListMultiplePeers(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	s.upsertPeer(context.Background(), 1, &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}, 0x6A, [4]byte{})
	s.upsertPeer(context.Background(), 2, &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 6000}, 0x6B, [4]byte{})

	peerList := s.buildPeerList()
	// Each peer entry = 4 (ID) + 4 (IP) + 2 (port) + 1 (mode) = 11 bytes
	if len(peerList) != 22 {
		t.Fatalf("expected 22 bytes for 2 peers, got %d", len(peerList))
	}
}

// --- SetBurstHandler ---

func TestSetBurstHandler(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	if s.burstHandler != nil {
		t.Fatal("expected nil burst handler initially")
	}

	handler := func(packetType byte, data []byte, addr *net.UDPAddr) {}
	s.SetBurstHandler(handler)

	if s.burstHandler == nil {
		t.Fatal("expected non-nil burst handler after set")
	}
}

// --- Full registration → alive → peer list integration test ---

func TestFullRegistrationFlow(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	clientAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	peerID := uint32(44444)
	preRegisterTestPeer(s, peerID)

	// Step 1: Register
	regReq := signTestPacket(t, makeControlPacketWithModeFlags(PacketType_MasterRegisterRequest, peerID, 0x6A, [4]byte{0, 0, 0, 0x0D}))
	_, err = s.handlePacket(context.Background(), regReq, clientAddr)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regReply := readUDP(t, client)
	if regReply[0] != byte(PacketType_MasterRegisterReply) {
		t.Fatal("expected register reply")
	}

	// Step 2: Keep-alive
	aliveReq := signTestPacket(t, makeControlPacket(PacketType_MasterAliveRequest, peerID))
	_, err = s.handlePacket(context.Background(), aliveReq, clientAddr)
	if err != nil {
		t.Fatalf("alive: %v", err)
	}
	aliveReply := readUDP(t, client)
	if aliveReply[0] != byte(PacketType_MasterAliveReply) {
		t.Fatal("expected alive reply")
	}

	// Step 3: Peer list
	peerListReq := signTestPacket(t, makeControlPacket(PacketType_PeerListRequest, peerID))
	_, err = s.handlePacket(context.Background(), peerListReq, clientAddr)
	if err != nil {
		t.Fatalf("peer list: %v", err)
	}
	peerListReply := readUDP(t, client)
	if peerListReply[0] != byte(PacketType_PeerListReply) {
		t.Fatal("expected peer list reply")
	}

	// Verify peer state
	s.mu.RLock()
	peer := s.peers[peerID]
	s.mu.RUnlock()

	if peer == nil {
		t.Fatal("expected peer to exist")
	}
	if !peer.RegistrationStatus {
		t.Fatal("expected registered")
	}
	if peer.KeepAliveReceived < 1 {
		t.Fatal("expected at least 1 keepalive")
	}
	if peer.Mode != 0x6A {
		t.Fatalf("expected mode 0x6A, got 0x%02X", peer.Mode)
	}
}

func TestHandleMasterRegisterRequestFlowExistingPeerLoadsAuthKeyFromDB(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDPAndDB(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	peerID := uint32(33333)
	if err := s.db.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(peerID)}, Type: models.RepeaterTypeIPSC, Password: "1234"}).Error; err != nil {
		t.Fatalf("failed to create test repeater: %v", err)
	}

	// Simulate a peer known to memory without an auth key.
	s.mu.Lock()
	s.peers[peerID] = &Peer{ID: peerID}
	s.mu.Unlock()

	reqData := signTestPacket(t, makeControlPacketWithModeFlags(PacketType_MasterRegisterRequest, peerID, 0x6A, [4]byte{0, 0, 0, 0x0D}))
	clientUDPAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}

	_, err = s.handlePacket(context.Background(), reqData, clientUDPAddr)
	if err != nil {
		t.Fatalf("handlePacket error: %v", err)
	}

	reply := readUDP(t, client)
	replyPayloadLen := len(s.buildMasterRegisterReply())
	if len(reply) != replyPayloadLen+10 {
		t.Fatalf("expected signed reply length %d, got %d", replyPayloadLen+10, len(reply))
	}

	if !authWithKey(reply, decodeAuthKey("1234")) {
		t.Fatal("expected signed register reply with peer auth key")
	}

	s.mu.RLock()
	peer := s.peers[peerID]
	s.mu.RUnlock()
	if peer == nil || peer.AuthKey == nil {
		t.Fatal("expected peer auth key to be cached")
	}
}

// --- handlePacket with MasterAliveRequest too short ---

func TestHandleMasterAliveRequestTooShort(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	data := []byte{byte(PacketType_MasterAliveRequest), 0x00, 0x01}
	aliveShortAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), data, aliveShortAddr)
	if err == nil {
		t.Fatal("expected error for too-short alive request")
	}
}

// --- handlePacket with MasterRegisterRequest too short for peer ID ---

func TestHandleMasterRegisterRequestTooShort(t *testing.T) {
	t.Parallel()
	s, srvAddr := newTestServerWithUDP(t)

	client, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	data := []byte{byte(PacketType_MasterRegisterRequest), 0x00}
	regShortAddr, ok := client.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("expected *net.UDPAddr from LocalAddr")
	}
	_, err = s.handlePacket(context.Background(), data, regShortAddr)
	if err == nil {
		t.Fatal("expected error for too-short register request")
	}
}

// --- Verify handlePacket returns Packet on success ---

func TestHandlePacketReturnsPacket(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	peerID := uint32(12345)
	preRegisterTestPeer(s, peerID)
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	payload := makeControlPacket(PacketType_RepeaterWakeUp, peerID)
	data := signTestPacket(t, payload)
	pkt, err := s.handlePacket(context.Background(), data, addr)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if pkt == nil {
		t.Fatal("expected non-nil packet")
	}
	if len(pkt.data) != len(payload) {
		t.Fatalf("expected packet data length %d, got %d", len(payload), len(pkt.data))
	}
}

// --- Verify handlePacket strips auth hash from returned data ---

func TestHandlePacketStripsAuthHash(t *testing.T) {
	t.Parallel()
	hexKey := "0000000000000000000000000000000000001234"
	cfg := testConfig()
	s := newTestServer(t, cfg)
	// Pre-register peer with auth key
	s.mu.Lock()
	s.peers[12345] = &Peer{ID: 12345, AuthKey: decodeAuthKey("1234"), RegistrationStatus: true}
	s.mu.Unlock()

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	payload := makeControlPacket(PacketType_RepeaterWakeUp, 12345)
	data := signPacket(t, payload, hexKey)

	pkt, err := s.handlePacket(context.Background(), data, addr)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The returned packet data should have the hash stripped
	if len(pkt.data) != len(payload) {
		t.Fatalf("expected data length %d (hash stripped), got %d", len(payload), len(pkt.data))
	}
}

// --- ErrPacketIgnored is a sentinel error ---

func TestErrPacketIgnoredIsSentinel(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	peerID := uint32(12345)
	preRegisterTestPeer(s, peerID)
	data := signTestPacket(t, makeControlPacket(PacketType_MasterRegisterReply, peerID))
	_, err := s.handlePacket(context.Background(), data, addr)
	if !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("expected ErrPacketIgnored, got %v", err)
	}
}

// --- Regression tests for ensurePeer deduplication ---

func TestEnsurePeerCreatesNewPeer(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	s.mu.Lock()
	peer := s.ensurePeer(100, addr)
	s.mu.Unlock()

	if peer == nil {
		t.Fatal("expected non-nil peer")
	}
	if peer.ID != 100 {
		t.Fatalf("expected peer ID 100, got %d", peer.ID)
	}
	if !peer.Addr.IP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("expected IP 10.0.0.1, got %v", peer.Addr.IP)
	}
	if peer.LastSeen.IsZero() {
		t.Fatal("expected LastSeen to be set")
	}
}

func TestEnsurePeerReusesExistingPeer(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr1 := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}
	addr2 := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 5678}

	s.mu.Lock()
	peer1 := s.ensurePeer(100, addr1)
	s.mu.Unlock()

	s.mu.Lock()
	peer2 := s.ensurePeer(100, addr2)
	s.mu.Unlock()

	// Should be the same peer object, updated with new address
	if peer1 != peer2 {
		t.Fatal("expected same peer object on re-ensure")
	}
	if !peer2.Addr.IP.Equal(net.IPv4(10, 0, 0, 2)) {
		t.Fatalf("expected updated IP, got %v", peer2.Addr.IP)
	}
	if s.peerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", s.peerCount())
	}
}

// --- Regression tests for DB write debouncing ---

func TestUpdateRepeaterDBTimesDebounce(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// With no DB configured, this should not panic or fail
	s.mu.Lock()
	s.updateRepeaterDBTimes(100, false)
	s.mu.Unlock()

	// Simulate the debounce mechanism: set a recent lastDBWrite
	s.mu.Lock()
	s.lastDBWrite[100] = time.Now()
	s.mu.Unlock()

	// A second keepalive update should be debounced (no DB = no-op, but the
	// debounce path is still exercised)
	s.mu.Lock()
	s.updateRepeaterDBTimes(100, false)
	s.mu.Unlock()
}

func TestUpdateRepeaterDBTimesConnectAlwaysWrites(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	// Set a very recent lastDBWrite to simulate debounce scenario
	s.mu.Lock()
	s.lastDBWrite[100] = time.Now()
	s.mu.Unlock()

	// Connect events (isConnect=true) should bypass debounce.
	// With no DB this is a no-op, but the code path is exercised.
	s.mu.Lock()
	s.updateRepeaterDBTimes(100, true)
	s.mu.Unlock()
}

func TestUpsertAndMarkPeerAliveShareEnsurePeer(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}

	// upsertPeer creates the peer first
	s.upsertPeer(context.Background(), 100, addr, 0x6A, [4]byte{})

	s.mu.RLock()
	registered := s.peers[100].RegistrationStatus
	s.mu.RUnlock()
	if !registered {
		t.Fatal("expected peer to be registered after upsert")
	}

	// markPeerAlive should reuse the same peer
	s.markPeerAlive(100, addr)

	s.mu.RLock()
	keepAlive := s.peers[100].KeepAliveReceived
	s.mu.RUnlock()
	if keepAlive != 1 {
		t.Fatalf("expected 1 keepalive, got %d", keepAlive)
	}

	// Peer count should still be 1
	if s.peerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", s.peerCount())
	}
}

func TestLastDBWriteMapInitialized(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s := newTestServer(t, cfg)
	if s.lastDBWrite == nil {
		t.Fatal("expected lastDBWrite map to be initialized")
	}
}

func FuzzParsePeerID(f *testing.F) {
	// Valid 5-byte packet with peer ID 311860 (0x0004C234)
	f.Add([]byte{0x90, 0x00, 0x04, 0xC2, 0x34})
	// Too short
	f.Add([]byte{0x90, 0x00})
	// Exactly 5 bytes, all zeros
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00})
	// Max uint32
	f.Add([]byte{0x80, 0xFF, 0xFF, 0xFF, 0xFF})
	// Empty
	f.Add([]byte{})
	// Longer packet (realistic IPSC size)
	long := make([]byte, 100)
	long[0] = 0x80
	binary.BigEndian.PutUint32(long[1:5], 12345)
	f.Add(long)
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		_, _ = parsePeerID(data)
	})
}

// --- Benchmarks ---

func BenchmarkAuthWithKey(b *testing.B) {
	key := []byte("secret-key-1234")
	// Build a signed IPSC packet
	payload := make([]byte, 54)
	payload[0] = 0x80
	binary.BigEndian.PutUint32(payload[1:5], 12345)
	hash := hmac.New(sha1.New, key)
	hash.Write(payload)
	hashSum := hash.Sum(nil)[:10]
	data := make([]byte, 0, len(payload)+len(hashSum))
	data = append(data, payload...)
	data = append(data, hashSum...)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authWithKey(data, key)
	}
}

func BenchmarkParsePeerID(b *testing.B) {
	data := make([]byte, 54)
	data[0] = 0x80
	binary.BigEndian.PutUint32(data[1:5], 311860)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parsePeerID(data)
	}
}
