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
	"encoding/hex"
	"fmt"
	"net"
	"testing"
)

// The control packets of a Motorola XPR 8400 (peer 0x0004CE2E) logging in to
// a master, captured on 2026-10-15 without their HMAC trailer
// (dmrgo-rfcap ipsc-capture-20261015-1228.log). The replies are the octets
// that DMRHub built by hand before it used dmrgo transport/ipsc, for master
// 311860 (0x0004C234).
//
//nolint:gochecknoglobals
var xprControlGolden = []struct {
	name, request, reply string
}{
	{"register", "900004ce2e6a0000805c04080400", "910004c2346a0000001d000104020401"},
	{"peer list", "920004ce2e", "930004c234000b0004ce2e7f00000100016a"},
	{"keepalive", "960004ce2e6a000080dc04020401", "970004c2346a0000001d04020401"},
	{"wakeup", "850004ce2e000000010002", ""},
}

func TestXPRControlGolden(t *testing.T) {
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
	preRegisterTestPeer(s, 0x0004CE2E)
	s.peers[0x0004CE2E].RegistrationStatus = false

	for _, tc := range xprControlGolden {
		if tc.name == "peer list" {
			tc.reply = fmt.Sprintf("%s%04x6a", tc.reply[:len(tc.reply)-6], clientAddr.Port)
		}
		req := signTestPacket(t, mustDecodeHex(t, tc.request))
		if _, err := s.handlePacket(context.Background(), req, clientAddr); err != nil {
			t.Fatalf("%s: handlePacket: %v", tc.name, err)
		}
		if tc.reply == "" {
			continue
		}
		got := readUDP(t, client)
		body, trailer := got[:len(got)-10], got[len(got)-10:]
		if hex.EncodeToString(body) != tc.reply {
			t.Fatalf("%s: reply %x, want %s", tc.name, body, tc.reply)
		}
		mac := hmac.New(sha1.New, mustDecodeHex(t, testHexKey))
		mac.Write(body)
		if !hmac.Equal(trailer, mac.Sum(nil)[:10]) {
			t.Fatalf("%s: reply trailer %x is not HMAC-SHA1 of the reply", tc.name, trailer)
		}
	}
}
